package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func (r *MarketDataRepo) InvestmentExchangeRates(ctx context.Context, tenant, actor, key, currency string, sources, dates []string) (nav.ExchangeRates, error) {
	out := nav.ExchangeRates{ReadAt: time.Now().UTC(), Currency: currency}
	if _, _, e := nav.ExchangeRateDates(currency, sources, dates, out.ReadAt); e != nil {
		return out, e
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = groupAccess(ctx, tx, tenant, actor, false); e != nil {
		return out, e
	}
	policy, points, e := readFXSnapshot(ctx, tx, tenant, key, out.ReadAt, time.Time{})
	if e != nil {
		return out, e
	}
	out.PolicyKey, out.PolicyVersion, out.Mode = policy.Key, policy.Version, policy.Mode
	fixings := make([]calc.FXFixing, 0, len(points))
	for _, point := range points {
		fixings = append(fixings, point.Fixing())
	}
	out.Rates, e = nav.ReportingExchangeRates(fixings, md.FXPolicyRef(policy.Key, policy.Version), currency, sources, dates, out.ReadAt)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
