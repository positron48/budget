package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// BenchmarkForSeries assembles the comparison from one authorized snapshot,
// including cash flows, boundary NAV, benchmark revisions, calendars and FX.
func (r *MarketDataRepo) BenchmarkForSeries(ctx context.Context, tenant, actor, series, benchmark, from, to string) (nav.BenchmarkComparison, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nav.BenchmarkComparison{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, e := readBenchmarkComparison(ctx, tx, tenant, actor, series, benchmark, from, to)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func readBenchmarkComparison(ctx context.Context, tx pgx.Tx, tenant, actor, series, benchmark, from, to string) (nav.BenchmarkComparison, error) {
	var out nav.BenchmarkComparison
	performance, e := readInvestmentPerformance(ctx, tx, tenant, actor, series, from, to)
	if e != nil {
		return out, e
	}
	out.Portfolio = performance
	out.Profile, e = readBenchmarkProfile(ctx, tx, tenant, benchmark)
	if e != nil {
		return out, e
	}
	if !out.Profile.Enabled {
		return out, d.ErrInvestmentPrecondition
	}
	profile, e := readPriceConventions(ctx, tx, tenant, out.Profile.PriceProfileKey)
	if e != nil {
		return out, e
	}
	if !profile.Enabled || len(profile.Periods[out.Profile.InstrumentKey]) == 0 {
		return out, d.ErrInvestmentPrecondition
	}
	out.PriceProfileVersion = profile.Version
	if len(performance.Days) == 0 || !performance.Days[0].Complete || performance.Days[0].NAV == nil {
		out.Result.Reason = "initial_nav_unknown"
		return out, nil
	}
	if performance.Days[0].NAV.Sign() < 0 {
		out.Result.Reason = "negative_nav"
		return out, nil
	}
	through, _ := nav.DayCutoff(to)
	fxPolicy, fxPoints, e := readFXSnapshot(ctx, tx, tenant, performance.FXPolicyKey, through, time.Time{})
	if e != nil && !errors.Is(e, md.ErrNotFound) {
		return out, e
	}
	policyRef := performance.FXPolicyKey
	fixings := []calc.FXFixing{}
	if e == nil {
		policyRef = md.FXPolicyRef(fxPolicy.Key, fxPolicy.Version)
		for _, point := range fxPoints {
			fixings = append(fixings, point.Fixing())
		}
	}
	cached := map[int][]md.ScopedObservation{}
	days := []calc.BenchmarkDay{}
	for _, day := range performance.Days {
		at, _ := nav.DayCutoff(day.Date)
		var period nav.PriceConventionPeriod
		periodIndex := -1
		for index, candidate := range profile.Periods[out.Profile.InstrumentKey] {
			if !at.Before(candidate.From) && (candidate.To.IsZero() || at.Before(candidate.To)) {
				period = candidate
				periodIndex = index
				break
			}
		}
		if periodIndex < 0 {
			days = append(days, calc.BenchmarkDay{Date: day.Date, ExternalFlow: day.ExternalFlow, HistoryReset: performance.ResetDates[day.Date]})
			out.Evidence = append(out.Evidence, nav.BenchmarkDailyProvenance{Date: day.Date, Issues: []string{"benchmark_convention_coverage_missing"}})
			continue
		}
		if period.Policy.Kind != "daily_candle" || period.Policy.Adjustment != "unadjusted" || period.Policy.SourceKind != "exchange" || period.Policy.Calendar == nil {
			return out, d.ErrInvestmentPrecondition
		}
		q := md.PriceSelectionRequest{Scope: period.Scope, InstrumentID: period.InstrumentID, Kind: "daily_candle", SourceKind: "exchange", Adjustment: "unadjusted", At: at, Calendar: period.Policy.Calendar}
		evidence, loaded := cached[periodIndex]
		if !loaded {
			// The linked price profile may have changed after benchmark registration.
			// Recheck semantic identity rather than trusting the original publication.
			expectedKind := "index"
			if out.Profile.Kind == "etf_price_proxy" {
				expectedKind = "etf"
			}
			var matches bool
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM market_provider_instruments WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 AND data->>'kind'=$5 AND (data->>'currency'='' OR data->>'currency'=$6))`, tenant, period.Provider, period.AccessScope, period.InstrumentID, expectedKind, out.Profile.Currency).Scan(&matches)
			if e != nil {
				return out, e
			}
			if !matches {
				return out, d.ErrInvestmentPrecondition
			}
			readQuery := q
			readQuery.At = through
			lower := period.From
			if period.Policy.Calendar.From.After(lower) {
				lower = period.Policy.Calendar.From
			}
			if !through.Before(lower) {
				evidence, e = readPriceEvidence(ctx, tx, readQuery, lower)
				if e != nil {
					return out, e
				}
			}
			cached[periodIndex] = evidence
		}
		selected, e := md.SelectValuationPrice(q, evidence)
		if e != nil {
			return out, e
		}
		unit := "index_points"
		if out.Profile.Kind == "etf_price_proxy" {
			unit = "currency_per_unit"
		}
		convention := nav.BenchmarkConvention{Key: out.Profile.Key, Title: out.Profile.Title, Kind: out.Profile.Kind, Currency: out.Profile.Currency, Unit: unit, Evidence: out.Profile.Evidence, Revision: out.Profile.Version, Confirmed: true, SourceKey: period.Provider + "/" + period.AccessScope, InstrumentID: period.InstrumentID, From: period.From, To: period.To}
		prepared, e := nav.PrepareBenchmark(convention, performance.Currency, policyRef, []nav.BenchmarkDailyEvidence{{Date: day.Date, Price: selected, ExternalFlow: day.ExternalFlow}}, fixings)
		if e != nil {
			return out, e
		}
		prepared.Days[0].HistoryReset = performance.ResetDates[day.Date]
		days = append(days, prepared.Days[0])
		out.Evidence = append(out.Evidence, prepared.Evidence[0])
	}
	out.Result, e = calc.BenchmarkPortfolio(performance.Currency, performance.Days[0].NAV, days)
	return out, e
}
