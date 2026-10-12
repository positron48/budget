package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"math/big"
	"time"
)

func (r *MarketDataRepo) OverviewForSeries(ctx context.Context, tenant, actor, key, from, to string) (nav.Overview, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nav.Overview{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, e := readInvestmentOverview(ctx, tx, tenant, actor, key, from, to, time.Now().UTC())
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func readInvestmentOverview(ctx context.Context, tx pgx.Tx, tenant, actor, key, from, to string, now time.Time) (nav.Overview, error) {
	out := nav.Overview{ReadAt: now}
	cutoff, e := nav.DayCutoff(to)
	if e != nil || cutoff.After(now) {
		return out, d.ErrInvestmentInvalid
	}
	out.Performance, e = readInvestmentPerformance(ctx, tx, tenant, actor, key, from, to)
	if e != nil {
		return out, e
	}
	timeline, e := calc.CalculateProfitTimeline(calc.PeriodPerformanceInput{Days: out.Performance.Days, FlowCoverage: true, ResetDates: out.Performance.ResetDates})
	if e != nil {
		return out, e
	}
	out.Daily = calc.ProfitPoint{Date: to, DailyReason: "terminal_nav_unknown"}
	for _, p := range timeline.Points {
		if p.Date == to {
			out.Daily = p
		}
	}
	q, _, _, e := readNAVRegisteredRequest(ctx, tx, tenant, actor, key)
	if e != nil {
		return out, e
	}
	days, e := readDailyNAVHistory(ctx, tx, q, to, to)
	if e != nil {
		return out, e
	}
	out.NAV = nav.DailyNAV{Date: to, State: "unavailable"}
	if len(days) > 0 {
		out.NAV = days[0]
	}
	if (out.NAV.State == "ready" || out.NAV.State == "incomplete") && out.NAV.Result != nil {
		// Whole selected account scope, never mislabel group valuation as its return.
		c, e := nav.ComposeComposition(*out.NAV.Result, nav.InstrumentGroups{}, nil, "account")
		if e != nil {
			return out, e
		}
		out.Composition = &c
	}
	policy, points, e := readFXSnapshot(ctx, tx, tenant, q.FXPolicyKey, now, time.Time{})
	if e != nil && !errors.Is(e, md.ErrNotFound) {
		return out, e
	}
	out.FX = overviewFX(policy, points, q.FXPolicyKey, now)
	return out, nil
}

// Widgets report only comparable direct/inverse observations from this single
// reviewed source policy. Their last stored values may be expired; they never
// substitute for historical NAV fixings or silently use another source.
func overviewFX(policy md.SavedFXPolicy, points []md.SavedFXPoint, key string, now time.Time) []nav.OverviewFX {
	out := []nav.OverviewFX{}
	for _, currency := range []string{"USD", "EUR"} {
		row := nav.OverviewFX{From: currency, To: "RUB", Policy: key, PolicyVersion: policy.Version, Source: policy.Source, Mode: policy.Mode, State: "unavailable"}
		var current, previous *md.SavedFXPoint
		for i := range points {
			p := &points[i]
			if ((p.From != currency || p.To != "RUB") && (p.From != "RUB" || p.To != currency)) || p.At.After(now) || p.AvailableAt.After(now) {
				continue
			}
			if current == nil || p.At.After(current.At) {
				previous = current
				current = p
			} else if p.At.Before(current.At) && (previous == nil || p.At.After(previous.At)) {
				previous = p
			}
		}
		rate := func(p *md.SavedFXPoint) *big.Rat {
			if p == nil {
				return nil
			}
			v, ok := new(big.Rat).SetString(p.Rate)
			if !ok || v.Sign() <= 0 {
				return nil
			}
			if p.From == "RUB" {
				v.Inv(v)
			}
			return v
		}
		consistent := func(selected *md.SavedFXPoint) *big.Rat {
			value := rate(selected)
			if value == nil {
				return nil
			}
			for i := range points {
				p := &points[i]
				if !p.At.Equal(selected.At) || p.AvailableAt.After(now) || ((p.From != currency || p.To != "RUB") && (p.From != "RUB" || p.To != currency)) {
					continue
				}
				candidate := rate(p)
				if candidate == nil || candidate.Cmp(value) != 0 {
					return nil
				}
			}
			return value
		}
		if current != nil {
			row.Rate = consistent(current)
			row.At = current.At
			row.AvailableAt = current.AvailableAt
			row.ValidUntil = current.ValidUntil
			row.Revision = current.Revision
			if row.Rate != nil {
				row.State = "fresh"
				if !now.Before(current.ValidUntil) {
					row.State = "expired"
				}
			}
			if old := consistent(previous); old != nil && row.Rate != nil {
				row.Change = new(big.Rat).Sub(new(big.Rat).Quo(row.Rate, old), big.NewRat(1, 1))
			}
		}
		out = append(out, row)
	}
	return out
}
