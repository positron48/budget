package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/positron48/budget/internal/adapter/nativehistory"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"math/big"
	"time"
)

// Built-in price benchmarks are read as one snapshot, independently of personal
// mappings. No provider requests, entitlement changes or financial writes occur.
func (r *MarketDataRepo) TrackerBenchmarks(ctx context.Context, tenant, actor string, q nav.TrackerHistoryRequest, baseDate string) ([]nav.TrackerBenchmark, error) {
	now := time.Now().UTC()
	q, e := nav.NormalizeTrackerHistory(q, now)
	if e != nil {
		return nil, e
	}
	start, _ := time.Parse(time.DateOnly, q.From)
	first := start.AddDate(0, 0, -1)
	requestedBase, e := time.Parse(time.DateOnly, baseDate)
	end, _ := time.Parse(time.DateOnly, q.To)
	if e != nil || requestedBase.Before(first) || requestedBase.After(end) {
		return nil, d.ErrInvestmentInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var role string
	e = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, tenant, actor).Scan(&role)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && role != "owner" && role != "admin" && role != "member" {
		return nil, d.ErrInvestmentForbidden
	}
	if e != nil {
		return nil, e
	}
	var accounts int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM investment_ledgers WHERE tenant_id=$1 AND id=ANY($2::uuid[]) AND portfolio_tracking`, tenant, q.LedgerIDs).Scan(&accounts); e != nil {
		return nil, e
	}
	if accounts != len(q.LedgerIDs) {
		return nil, d.ErrInvestmentNotFound
	}
	through, _ := nav.DayCutoff(q.To)
	policy, points, e := readFXSnapshot(ctx, tx, tenant, q.FXPolicyKey, through, time.Time{})
	if e != nil {
		return nil, e
	}
	fixings := []calc.FXFixing{}
	for _, p := range points {
		if !p.AvailableAt.After(through) {
			fixings = append(fixings, p.Fixing())
		}
	}
	fxSeries, e := calc.NewFXSeries(fixings, md.FXPolicyRef(policy.Key, policy.Version))
	if e != nil {
		return nil, e
	}

	entries := map[string]trackerHistoryMarket{}
	for _, b := range nativehistory.Benchmarks() {
		entries[b.Key] = trackerHistoryMarket{mapping: md.Mapping{InstrumentKey: b.Key, Version: 1, HistoricalPeriods: []md.MappingPeriod{b.Period()}}}
	}
	firstAt, _ := nav.DayCutoff(first.Format(time.DateOnly))
	entries, e = readTrackerHistorySeries(ctx, tx, tenant, entries, firstAt, through, now)
	if e != nil {
		return nil, e
	}
	result := []nav.TrackerBenchmark{}
	for _, b := range nativehistory.Benchmarks() {
		base := requestedBase
		line := nav.TrackerBenchmark{Key: b.Key, Title: b.Title, Kind: b.Kind, NativeCurrency: b.Instrument.Currency, Provider: b.Provider, SourceURL: b.URL, BaseDate: baseDate, ReadAt: now}
		entry := entries[b.Key]
		p := b.Period()
		var baseline *big.Rat
		for day := first; !day.After(end); day = day.AddDate(0, 0, 1) {
			point := nav.TrackerBenchmarkPoint{Date: day.Format(time.DateOnly)}
			cutoff, _ := nav.DayCutoff(point.Date)
			selected, err := entry.selectPrice(historyPriceSelection(tenant, p, cutoff), historyPeriodKey(p))
			if err != nil {
				return nil, err
			}
			var value *big.Rat
			if selected.Fact == nil {
				point.Reason = "benchmark_price_missing"
			} else if selected.Fact.Currency != b.Instrument.Currency || selected.Fact.Unit != b.Unit || selected.Fact.Adjustment != "unadjusted" {
				point.Reason = "benchmark_terms_conflict"
			} else {
				resolver, err := fxSeries.At(cutoff)
				if err != nil {
					return nil, err
				}
				rate, found := resolver.Resolve(b.Instrument.Currency, q.Currency)
				if !found {
					point.Reason = "benchmark_fx_missing"
				} else {
					v, ok := new(big.Rat).SetString(selected.Fact.Price)
					if !ok || v.Sign() <= 0 {
						return nil, d.ErrInvestmentPrecondition
					}
					value = new(big.Rat).Mul(v, rate.Rate)
				}
			}
			if !day.Before(base) && baseline == nil && value != nil {
				base = day
				line.BaseDate = point.Date
			}
			if point.Date == line.BaseDate {
				if value != nil {
					baseline = new(big.Rat).Set(value)
					line.Reason = ""
				} else {
					line.Reason = point.Reason
				}
			}
			if !day.Before(base) {
				if baseline == nil {
					point.Reason = "initial_benchmark_price_unavailable"
				} else if value != nil {
					index := new(big.Rat).Mul(new(big.Rat).Quo(value, baseline), big.NewRat(100, 1))
					point.Index, _ = new(big.Rat).SetString(index.FloatString(12))
					if point.Index.Sign() <= 0 {
						point.Index, point.Reason = nil, "benchmark_numerical_range"
					}
				}
			}
			if point.Reason != "" && !day.Before(base) && line.Reason == "" {
				line.Reason = "incomplete"
			}
			line.Points = append(line.Points, point)
		}
		result = append(result, line)
	}
	return result, tx.Commit(ctx)
}
