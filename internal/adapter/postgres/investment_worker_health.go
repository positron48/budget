package postgres

import (
	"context"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"github.com/positron48/budget/internal/usecase/workerhealth"
)

// Due age starts at available_at (or expired lease), not creation time: provider
// Retry-After and an active lease must not look like stalled work.
const marketWorkerHealthSQL = `WITH jobs AS (
 SELECT kind,status,available_at,lease_until FROM market_data_jobs
 WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3
), due AS (
 SELECT *,CASE WHEN status='pending' THEN available_at WHEN status='running' THEN lease_until END AS due_at FROM jobs
)
SELECT kind,status,count(*),count(*) FILTER(WHERE due_at<=statement_timestamp()),
 count(*) FILTER(WHERE status='running' AND lease_until<=statement_timestamp()),
 COALESCE(GREATEST(EXTRACT(epoch FROM statement_timestamp()-min(due_at)),0),0)::double precision
FROM due GROUP BY kind,status
UNION ALL
SELECT 'source','enabled',COALESCE((SELECT enabled::int FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3),0),0,0,0`

const navWorkerHealthSQL = `WITH jobs AS (
 SELECT status,lease_until,CASE WHEN status='pending' THEN available_at WHEN status='running' THEN lease_until END AS due_at
 FROM investment_nav_jobs WHERE tenant_id=$1
), targets AS (
 SELECT DISTINCT w.tenant_id,w.series_key,w.from_date,
 CASE WHEN EXISTS(SELECT 1 FROM investment_portfolios p WHERE p.tenant_id=w.tenant_id AND p.actor_id=w.actor_id
 AND (p.data->>'SeriesKey'=w.series_key OR (COALESCE(w.request->>'ConventionKey','')<>''
 AND p.data->>'ConventionKey'=w.request->>'ConventionKey' AND (p.data->'LedgerIDs') @> (w.request->'LedgerIDs')
 AND (w.request->'LedgerIDs') @> (p.data->'LedgerIDs')))
 AND p.data->>'AutoExtend'='true' AND COALESCE(p.data->>'Archived','false')='false')
 THEN LEAST(w.from_date+3660,(statement_timestamp() AT TIME ZONE 'Europe/Moscow')::date-1)
 ELSE LEAST(w.to_date,(statement_timestamp() AT TIME ZONE 'Europe/Moscow')::date-1) END AS report_date
 FROM investment_nav_watches w JOIN user_tenants u ON u.tenant_id=w.tenant_id AND u.user_id=w.actor_id
 WHERE w.tenant_id=$1 AND w.enabled AND u.role IN ('owner','admin','member')
), quality AS (
 SELECT CASE WHEN n.series_key IS NULL THEN 'unavailable'
 WHEN g.generation IS NULL OR n.generation>g.generation OR EXISTS(
 SELECT 1 FROM investment_nav_invalidations i WHERE i.tenant_id=n.tenant_id AND i.generation>n.generation
 AND (i.affected_from IS NULL OR i.affected_from<=((n.report_date+1)::timestamp AT TIME ZONE 'Europe/Moscow')-interval '1 microsecond')) THEN 'stale'
 WHEN n.data->>'Complete'='true' THEN 'ready' ELSE 'incomplete' END AS state
 FROM targets t LEFT JOIN investment_daily_nav n ON n.tenant_id=t.tenant_id AND n.series_key=t.series_key AND n.report_date=t.report_date
 LEFT JOIN investment_valuation_generations g ON g.tenant_id=t.tenant_id WHERE t.report_date>=t.from_date
)
SELECT 'daily_nav',status,count(*),count(*) FILTER(WHERE due_at<=statement_timestamp()),
 count(*) FILTER(WHERE status='running' AND lease_until<=statement_timestamp()),
 COALESCE(GREATEST(EXTRACT(epoch FROM statement_timestamp()-min(due_at)),0),0)::double precision
FROM jobs GROUP BY status
UNION ALL SELECT 'valuation',state,count(*),0,0,0 FROM quality GROUP BY state`

func (r *MarketDataRepo) ReadMarketWorkerHealth(ctx context.Context, scope md.Scope) (workerhealth.Snapshot, error) {
	if err := md.ValidateScope(scope); err != nil {
		return workerhealth.Snapshot{}, err
	}
	return r.readWorkerHealth(ctx, marketWorkerHealthSQL, scope.TenantID, scope.Provider, scope.AccessScope)
}

func (r *MarketDataRepo) ReadNAVWorkerHealth(ctx context.Context, tenant string) (workerhealth.Snapshot, error) {
	if id, err := uuid.Parse(tenant); err != nil || id.String() != tenant {
		return workerhealth.Snapshot{}, d.ErrInvestmentInvalid
	}
	return r.readWorkerHealth(ctx, navWorkerHealthSQL, tenant)
}

func (r *MarketDataRepo) readWorkerHealth(ctx context.Context, query string, args ...any) (workerhealth.Snapshot, error) {
	var result workerhealth.Snapshot
	rows, err := r.pool.DB.Query(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var q workerhealth.Queue
		if err = rows.Scan(&q.Kind, &q.Status, &q.Count, &q.Due, &q.ExpiredLeases, &q.OldestDueSeconds); err != nil {
			return workerhealth.Snapshot{}, err
		}
		switch q.Kind {
		case "source":
			enabled := q.Count == 1
			result.SourceEnabled = &enabled
		case "valuation":
			if result.NAVStates == nil {
				result.NAVStates = map[string]int64{}
			}
			result.NAVStates[q.Status] = q.Count
		default:
			result.Queues = append(result.Queues, q)
		}
	}
	return result, rows.Err()
}
