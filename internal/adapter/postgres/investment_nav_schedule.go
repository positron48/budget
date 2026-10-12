package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

// ScheduleDailyNAV consumes committed invalidations for registered ranges.
// Watch locks serialize schedulers. Jobs and cursor commit atomically; a running
// job receives a higher requested generation and must run again after its lease.
func (r *MarketDataRepo) ScheduleDailyNAV(ctx context.Context, tenant string) (int, error) {
	return r.scheduleDailyNAVAt(ctx, tenant, time.Now())
}
func (r *MarketDataRepo) scheduleDailyNAVAt(ctx context.Context, tenant string, now time.Time) (int, error) {
	if id, e := uuid.Parse(tenant); e != nil || id.String() != tenant {
		return 0, d.ErrInvestmentInvalid
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, e := tx.Query(ctx, `SELECT w.actor_id,w.series_key,w.request,w.from_date::text,w.to_date::text,w.cursor_generation,
 EXISTS(SELECT 1 FROM investment_portfolios p WHERE p.tenant_id=w.tenant_id AND p.actor_id=w.actor_id AND (p.data->>'SeriesKey'=w.series_key OR (
 COALESCE(w.request->>'ConventionKey','')<>'' AND p.data->>'ConventionKey'=w.request->>'ConventionKey'
 AND (p.data->'LedgerIDs') @> (w.request->'LedgerIDs') AND (w.request->'LedgerIDs') @> (p.data->'LedgerIDs')
 )) AND p.data->>'AutoExtend'='true' AND COALESCE(p.data->>'Archived','false')='false')
 FROM investment_nav_watches w JOIN user_tenants u ON u.tenant_id=w.tenant_id AND u.user_id=w.actor_id
 WHERE w.tenant_id=$1 AND w.enabled AND u.role IN ('owner','admin','member') ORDER BY w.actor_id,w.series_key FOR UPDATE OF w SKIP LOCKED`, tenant)
	if e != nil {
		return 0, e
	}
	type watch struct {
		actor, key, from, to string
		raw                  []byte
		cursor               int64
		rolling              bool
	}
	var watches []watch
	for rows.Next() {
		var w watch
		if e = rows.Scan(&w.actor, &w.key, &w.raw, &w.from, &w.to, &w.cursor, &w.rolling); e != nil {
			rows.Close()
			return 0, e
		}
		watches = append(watches, w)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	var generation int64
	if e = tx.QueryRow(ctx, `SELECT COALESCE((SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1),0)`, tenant).Scan(&generation); e != nil {
		return 0, e
	}
	count := 0
	for _, w := range watches {
		if w.rolling {
			from, _ := time.Parse(time.DateOnly, w.from)
			capDate := from.AddDate(0, 0, 3660).Format(time.DateOnly)
			target := nav.LastCompletedDate(now)
			limited := target > capDate
			if limited {
				target = capDate
			}
			if target > w.to {
				// Only newly completed dates are queued here. Old dates are still
				// handled below by the invalidation cursor, independently of time.
				tag, err := tx.Exec(ctx, `INSERT INTO investment_nav_jobs(tenant_id,actor_id,series_key,request,report_date,requested_generation)
 SELECT $1,$2,$3,$4,$5::date+i,$7 FROM generate_series(1,$6::date-$5::date) days(i)
 ON CONFLICT(tenant_id,actor_id,series_key,report_date) WHERE status IN ('pending','running')
 DO UPDATE SET requested_generation=GREATEST(investment_nav_jobs.requested_generation,EXCLUDED.requested_generation)`, tenant, w.actor, w.key, w.raw, w.to, target, generation)
				if err != nil {
					return 0, err
				}
				count += int(tag.RowsAffected())
				w.to = target
			}
			if _, e = tx.Exec(ctx, `UPDATE investment_nav_watches SET to_date=$4::date,extension_limited=$5 WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3`, tenant, w.actor, w.key, w.to, limited); e != nil {
				return 0, e
			}
		} else {
			if _, e = tx.Exec(ctx, `UPDATE investment_nav_watches SET extension_limited=false WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3 AND extension_limited`, tenant, w.actor, w.key); e != nil {
				return 0, e
			}
		}
		if generation <= w.cursor {
			continue
		}
		var global bool
		var earliest *time.Time
		if e = tx.QueryRow(ctx, `SELECT COALESCE(bool_or(affected_from IS NULL),false),min(affected_from) FROM investment_nav_invalidations WHERE tenant_id=$1 AND generation>$2 AND generation<=$3`, tenant, w.cursor, generation).Scan(&global, &earliest); e != nil {
			return 0, e
		}
		// Initial subscription always builds the whole requested range, even if
		// all retained events happen after that range.
		if global || earliest != nil || w.cursor == 0 {
			tag, err := tx.Exec(ctx, `INSERT INTO investment_nav_jobs(tenant_id,actor_id,series_key,request,report_date,requested_generation)
 SELECT $1,$2,$3,$4,$5::date+i,$7 FROM generate_series(0,$6::date-$5::date) days(i)
 WHERE $8 OR $9::timestamptz<=((($5::date+i)+1)::timestamp AT TIME ZONE 'Europe/Moscow')-interval '1 microsecond'
 ON CONFLICT(tenant_id,actor_id,series_key,report_date) WHERE status IN ('pending','running')
 DO UPDATE SET requested_generation=GREATEST(investment_nav_jobs.requested_generation,EXCLUDED.requested_generation)`, tenant, w.actor, w.key, w.raw, w.from, w.to, generation, global || w.cursor == 0, earliest)
			if err != nil {
				return 0, err
			}
			count += int(tag.RowsAffected())
		}
		if _, e = tx.Exec(ctx, `UPDATE investment_nav_watches SET cursor_generation=$4 WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3`, tenant, w.actor, w.key, generation); e != nil {
			return 0, e
		}
	}
	return count, tx.Commit(ctx)
}
