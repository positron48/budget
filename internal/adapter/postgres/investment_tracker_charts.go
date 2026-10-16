package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"time"
)

// Payloads contain derived daily facts and period totals, never snapshot tokens.
// Callers must authorize and fence the current basis before using a projection.
func (r *MarketDataRepo) ReadTrackerChart(ctx context.Context, tenant, actor, scope, basis string) ([]byte, error) {
	var raw []byte
	err := r.pool.DB.QueryRow(ctx, `UPDATE investment_tracker_charts SET requested_at=now()
 WHERE tenant_id=$1 AND actor_id=$2 AND scope_key=$3 AND basis_key=$4 AND computed_at>now()-interval '24 hours'
 RETURNING projection`, tenant, actor, scope, basis).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return raw, err
}
func (r *MarketDataRepo) SaveTrackerChart(ctx context.Context, tenant, actor, scope, basis string, request, projection []byte, touch bool) error {
	if len(projection) > 16<<20 || len(request) > 65536 {
		return nil
	}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialise eviction per owner, retaining at most 24 requested views.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+actor); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO investment_tracker_charts(tenant_id,actor_id,scope_key,basis_key,request,projection)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,actor_id,scope_key) DO UPDATE
 SET basis_key=excluded.basis_key,request=excluded.request,projection=excluded.projection,computed_at=now(),requested_at=CASE WHEN $7 THEN now() ELSE investment_tracker_charts.requested_at END`, tenant, actor, scope, basis, request, projection, touch); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM investment_tracker_charts WHERE tenant_id=$1 AND actor_id=$2 AND scope_key IN
 (SELECT scope_key FROM investment_tracker_charts WHERE tenant_id=$1 AND actor_id=$2 ORDER BY requested_at DESC,scope_key OFFSET 24)`, tenant, actor); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Refresh only recently requested views. A revoked membership is rechecked by
// the same public calculation path and cannot receive previously cached facts.
func (r *MarketDataRepo) TrackerChartsToRefresh(ctx context.Context) ([]nav.TrackerChartRefresh, error) {
	rows, err := r.pool.DB.Query(ctx, `SELECT tenant_id,actor_id,request,computed_at FROM investment_tracker_charts
 WHERE (computed_at<$1 OR computed_at<date_trunc('day',now() AT TIME ZONE 'Europe/Moscow') AT TIME ZONE 'Europe/Moscow') AND requested_at>$2 ORDER BY computed_at LIMIT 24`, time.Now().Add(-6*time.Hour), time.Now().Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []nav.TrackerChartRefresh{}
	for rows.Next() {
		var v nav.TrackerChartRefresh
		if err = rows.Scan(&v.Tenant, &v.Actor, &v.Request, &v.ComputedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
