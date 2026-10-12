package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// EnqueueHistory is an operator-only, atomic backfill. Existing terminal jobs
// remain evidence, including errors and empty answers; resuming never silently
// turns them into successful coverage or retries exhausted provider requests.
func (r *MarketDataRepo) EnqueueHistory(ctx context.Context, scope md.Scope, id string, from, to time.Time) ([]md.Job, error) {
	if err := md.ValidateScope(scope); err != nil {
		return nil, err
	}
	requests, err := md.PlanHistory(id, from, to)
	if err != nil {
		return nil, err
	}
	if to.After(time.Now().UTC()) {
		return nil, md.ErrInvalid
	}
	return r.enqueueHistory(ctx, scope, requests, false)
}

// RequestHistory exposes a bounded, atomic public batch. Existing exhausted
// requests stay visible; this is hydration, not an implicit error reset.
func (r *MarketDataRepo) RequestHistory(ctx context.Context, scope md.Scope, q md.JobRequest) ([]md.Job, error) {
	if md.ValidateScope(scope) != nil || q.To.After(time.Now().UTC()) {
		return nil, md.ErrInvalid
	}
	requests, err := md.PlanRequestedHistory(q)
	if err != nil {
		return nil, err
	}
	return r.enqueueHistory(ctx, scope, requests, true)
}

func (r *MarketDataRepo) enqueueHistory(ctx context.Context, scope md.Scope, requests []md.JobRequest, public bool) ([]md.Job, error) {
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Shares the public refresh lock, so its active-job limit cannot race a backfill.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market-refresh:"+scope.TenantID); err != nil {
		return nil, err
	}
	var enabled bool
	err = tx.QueryRow(ctx, `SELECT enabled FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 FOR SHARE`, scope.TenantID, scope.Provider, scope.AccessScope).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !enabled {
		return nil, md.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var active, recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status IN ('pending','running')),count(*) FILTER(WHERE created_at>clock_timestamp()-interval '1 minute') FROM market_data_jobs WHERE tenant_id=$1`, scope.TenantID).Scan(&active, &recent); err != nil {
		return nil, err
	}
	jobs := make([]md.Job, 0, len(requests))
	for _, q := range requests {
		job, e := scanMarketJob(tx.QueryRow(ctx, `SELECT `+marketJobColumns+` FROM market_data_jobs WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 AND kind='daily_candle' AND from_at=$5 AND to_at=$6 ORDER BY (status IN ('pending','running')) DESC,created_at DESC,id DESC LIMIT 1`, scope.TenantID, scope.Provider, scope.AccessScope, q.InstrumentID, q.From, q.To))
		if errors.Is(e, pgx.ErrNoRows) {
			if active >= 100 || public && recent >= 10 {
				return nil, md.ErrLimit
			}
			job, e = scanMarketJob(tx.QueryRow(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,priority) VALUES($1,$2,$3,$4,'daily_candle',$5,$6,30) ON CONFLICT(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,search_query,instrument_kind) WHERE status IN ('pending','running') DO UPDATE SET id=market_data_jobs.id RETURNING `+marketJobColumns, scope.TenantID, scope.Provider, scope.AccessScope, q.InstrumentID, q.From, q.To))
			active++
		}
		if e != nil {
			return nil, e
		}
		jobs = append(jobs, job)
	}
	if public {
		if err = touchMarketSubscription(ctx, tx, scope, requests[len(requests)-1]); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return jobs, nil
}
