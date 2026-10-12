package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

func readNAVVariant(ctx context.Context, tx pgx.Tx, tenant, actor, source, currency, fx string) (nav.Variant, nav.Request, error) {
	var out nav.Variant
	q, from, to, e := readNAVRegisteredRequest(ctx, tx, tenant, actor, source)
	if e != nil {
		return out, q, e
	}
	if (currency != "RUB" && currency != "USD" && currency != "EUR") || len(fx) == 0 || len(fx) > 100 {
		return out, q, d.ErrInvestmentInvalid
	}
	var enabled bool
	e = tx.QueryRow(ctx, `SELECT enabled FROM investment_fx_policies WHERE tenant_id=$1 AND policy_key=$2`, tenant, fx).Scan(&enabled)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && !enabled {
		return out, q, d.ErrInvestmentPrecondition
	}
	if e != nil {
		return out, q, e
	}
	q.Currency = currency
	q.FXPolicyKey = fx
	key, _, e := navSeries(q)
	if e != nil {
		return out, q, e
	}
	out.Series = nav.SeriesSummary{Key: key, LedgerIDs: append([]string(nil), q.LedgerIDs...), Currency: currency, FXPolicyKey: fx, From: from, To: to}
	e = tx.QueryRow(ctx, `SELECT enabled AND from_date<=$4::date AND to_date>=$5::date FROM investment_nav_watches WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3`, tenant, actor, key, from, to).Scan(&out.Registered)
	if errors.Is(e, pgx.ErrNoRows) {
		e = nil
	}
	return out, q, e
}
func (r *MarketDataRepo) NAVVariant(ctx context.Context, tenant, actor, source, currency, fx string) (nav.Variant, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nav.Variant{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, _, e := readNAVVariant(ctx, tx, tenant, actor, source, currency, fx)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// PrepareNAVVariant registers a derived cache/watch, not a portfolio or journal
// mutation. The deterministic series key makes retries and concurrent clicks
// idempotent, and registration commits atomically with initial daily jobs.
func (r *MarketDataRepo) PrepareNAVVariant(ctx context.Context, tenant, actor, source, currency, fx string) (nav.Variant, error) {
	var out nav.Variant
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var role string
	e = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, tenant, actor).Scan(&role)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && (role != "owner" && role != "admin" && role != "member") {
		return out, d.ErrInvestmentForbidden
	}
	if e != nil {
		return out, e
	}
	// Shared lock order with portfolio save/archive prevents reactivation from an
	// archived source while that source is being disabled by the same actor.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "investment-portfolios:"+tenant+":"+actor); e != nil {
		return out, e
	}
	var q nav.Request
	out, q, e = readNAVVariant(ctx, tx, tenant, actor, source, currency, fx)
	if e != nil {
		return out, e
	}
	var active bool
	if e = tx.QueryRow(ctx, `SELECT enabled FROM investment_nav_watches WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3`, tenant, actor, source).Scan(&active); e != nil {
		return out, e
	}
	if !active {
		return out, d.ErrInvestmentPrecondition
	}
	if !out.Registered {
		if q.ConventionKey != "" {
			profile, err := readPriceConventions(ctx, tx, tenant, q.ConventionKey)
			if err != nil {
				return out, err
			}
			if !profile.Enabled {
				return out, d.ErrInvestmentPrecondition
			}
		}
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM investment_ledgers WHERE tenant_id=$1 AND id=ANY($2::uuid[])`, tenant, q.LedgerIDs).Scan(&count); e != nil {
			return out, e
		}
		if count != len(q.LedgerIDs) {
			return out, d.ErrInvestmentPrecondition
		}
		out.QueuedDays, e = enqueueDailyNAV(ctx, tx, q, out.Series.From, out.Series.To, false)
		if e != nil {
			return out, e
		}
		out.Registered = true
	}
	return out, tx.Commit(ctx)
}
