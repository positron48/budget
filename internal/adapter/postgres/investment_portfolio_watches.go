package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Caller serializes portfolio edits for this actor. Watch locks also serialize
// this reconciliation against the scheduler. Explicit internal watches remain
// enabled independently of portfolio ownership; no financial history is deleted.
func reconcilePortfolioWatches(ctx context.Context, tx pgx.Tx, tenant, actor string) error {
	rows, e := tx.Query(ctx, `SELECT series_key FROM investment_nav_watches WHERE tenant_id=$1 AND actor_id=$2 AND portfolio_managed ORDER BY series_key FOR UPDATE`, tenant, actor)
	if e != nil {
		return e
	}
	var keys []string
	for rows.Next() {
		var key string
		if e = rows.Scan(&key); e != nil {
			rows.Close()
			return e
		}
		keys = append(keys, key)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, key := range keys {
		var active bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_portfolios WHERE tenant_id=$1 AND actor_id=$2 AND data->>'SeriesKey'=$3 AND COALESCE((data->>'Archived')::boolean,false)=false)`, tenant, actor, key).Scan(&active)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE investment_nav_watches SET enabled=$4 WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3`, tenant, actor, key, active); e != nil {
			return e
		}
		if !active {
			if _, e = tx.Exec(ctx, `UPDATE investment_nav_jobs SET status='cancelled',error_code='subscription_inactive',lease_token=NULL,lease_until=NULL WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3 AND status IN ('pending','running')`, tenant, actor, key); e != nil {
				return e
			}
		}
	}
	return nil
}
