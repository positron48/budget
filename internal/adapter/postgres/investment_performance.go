package postgres

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// PerformanceForSeries reads authorization, generation, journal, FX and NAV
// invalidations from one snapshot. No broker API or journal mutation occurs.
func (r *MarketDataRepo) PerformanceForSeries(ctx context.Context, tenant, actor, key, from, to string) (nav.Performance, error) {
	tx, err := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nav.Performance{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, err := readInvestmentPerformance(ctx, tx, tenant, actor, key, from, to)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func readInvestmentPerformance(ctx context.Context, tx pgx.Tx, tenant, actor, key, from, to string) (nav.Performance, error) {
	out := nav.Performance{SeriesKey: key}
	q, start, end, err := readNAVRegisteredRequest(ctx, tx, tenant, actor, key)
	if err != nil {
		return out, err
	}
	fromDate, err := time.Parse(time.DateOnly, from)
	if err != nil || fromDate.Format(time.DateOnly) != from {
		return out, d.ErrInvestmentInvalid
	}
	through, err := nav.DayCutoff(to)
	if err != nil || from < start || to > end || to < from {
		return out, d.ErrInvestmentInvalid
	}
	baselineDate := fromDate.AddDate(0, 0, -1).Format(time.DateOnly)
	baseline, err := nav.DayCutoff(baselineDate)
	if err != nil {
		return out, d.ErrInvestmentInvalid
	}
	// Including baseline stays inside the established 3661-point read bound.
	toDate, _ := time.Parse(time.DateOnly, to)
	if (toDate.Unix()-fromDate.Unix())/86400 > 3659 {
		return out, d.ErrInvestmentLimit
	}
	out.Currency = q.Currency
	out.FXPolicyKey = q.FXPolicyKey
	if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1),0)`, tenant).Scan(&out.Generation); err != nil {
		return out, err
	}
	history, err := readDailyNAVHistoryMode(ctx, tx, q, baselineDate, to, true)
	if err != nil {
		return out, err
	}
	// NAV(s-1) may lie just outside the watch range. Calculate only this
	// boundary in the same snapshot when its projection is absent or stale.
	// This read does not publish a new projection or infer pre-ledger history.
	if len(history) > 0 && history[0].State != "ready" {
		baselineRequest := q
		baselineRequest.At = baseline
		initial, e := readInvestmentValuation(ctx, tx, baselineRequest)
		if e != nil {
			return out, e
		}
		history[0] = dailyPoint(baselineDate, initial.DataGeneration, time.Time{}, initial)
	}
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return out, err
	}
	operations := []d.InvestmentOperation{}
	resets := map[string]bool{}
	for _, ledger := range q.LedgerIDs {
		ops, e := loadInvestmentOperationsInPeriod(ctx, tx, tenant, ledger, baseline, through)
		if e != nil {
			return out, e
		}
		for _, op := range ops {
			if !op.OccurredAt.After(baseline) || op.OccurredAt.After(through) {
				continue
			}
			operations = append(operations, op)
			if op.Status == "posted" && (op.Kind == "opening_cash" || op.Kind == "opening_position") {
				resets[op.OccurredAt.In(location).Format(time.DateOnly)] = true
			}
		}
	}
	priceCache := map[string]nav.InstrumentPriceDay{}
	flows, err := calc.ExternalFlowsWithSecurities(q.LedgerIDs, operations, func(op d.InvestmentOperation) (calc.SecurityFlowValue, error) {
		date := op.OccurredAt.In(location).Format(time.DateOnly)
		cacheKey := op.InstrumentKey + "\x00" + date
		p, exists := priceCache[cacheKey]
		if !exists {
			history, e := readInvestmentInstrumentPriceDays(ctx, tx, tenant, q, op.InstrumentKey, date, date, nil, nav.InstrumentGroups{}, false)
			if e != nil {
				return calc.SecurityFlowValue{}, e
			}
			if len(history.Days) != 1 {
				return calc.SecurityFlowValue{}, d.ErrInvestmentPrecondition
			}
			p = history.Days[0]
			priceCache[cacheKey] = p
		}
		if p.NativeDirty == nil {
			return calc.SecurityFlowValue{}, nil
		}
		quantity, ok := new(big.Rat).SetString(op.Quantity)
		if !ok {
			return calc.SecurityFlowValue{}, d.ErrInvestmentInvalid
		}
		return calc.SecurityFlowValue{Value: new(big.Rat).Mul(quantity, p.NativeDirty), Currency: p.NativeCurrency, Date: date, SourceID: p.PriceEvidence}, nil
	})
	if err != nil {
		return out, err
	}
	// Load corrected fixing history once. The resolver applies the individual
	// day's availability and expiration; later observations never leak backward.
	policy, points, err := readFXSnapshot(ctx, tx, tenant, q.FXPolicyKey, through, time.Time{})
	missing := errors.Is(err, md.ErrNotFound)
	if err != nil && !missing {
		return out, err
	}
	fixings := []calc.FXFixing{}
	for _, point := range points {
		fixings = append(fixings, point.Fixing())
	}
	cache := map[string]*calc.FXResolver{}
	out.Flows, err = calc.ConvertExternalFlows(flows, q.Currency, location, func(date string) (*calc.FXResolver, error) {
		if missing {
			return nil, nil
		}
		if cached := cache[date]; cached != nil {
			return cached, nil
		}
		cutoff, e := nav.DayCutoff(date)
		if e != nil {
			return nil, e
		}
		resolver, e := calc.NewFXResolver(fixings, md.FXPolicyRef(policy.Key, policy.Version), cutoff)
		if e == nil {
			cache[date] = resolver
		}
		return resolver, e
	})
	if err != nil {
		return out, err
	}
	totals, err := calc.DailyExternalFlows(from, to, out.Flows)
	if err != nil {
		return out, err
	}
	days := make([]calc.PerformanceDay, 0, len(history))
	for _, point := range history {
		day := calc.PerformanceDay{Date: point.Date, ExternalFlow: totals[point.Date]}
		if point.State == "ready" && point.Result != nil && point.Result.Complete {
			day.NAV = point.Result.KnownValue
			day.Complete = true
		}
		days = append(days, day)
	}
	out.Days = days
	out.ResetDates = resets
	out.PeriodPerformance, err = calc.CalculatePeriod(calc.PeriodPerformanceInput{Days: days, FlowCoverage: true, ResetDates: resets})
	return out, err
}
