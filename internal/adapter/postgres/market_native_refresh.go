package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// Native sources serve daily history only. They must never receive the live
// broker's last-price, dividend, or present-day bond-facts jobs.
func (r *MarketDataRepo) scheduleNativeRefresh(ctx context.Context, s md.Scope, at time.Time, limit int) (int, error) {
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market-refresh:"+s.TenantID); e != nil {
		return 0, e
	}
	var enabled bool
	e = tx.QueryRow(ctx, `SELECT enabled FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 FOR SHARE`, s.TenantID, s.Provider, s.AccessScope).Scan(&enabled)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && !enabled {
		return 0, tx.Commit(ctx)
	}
	if e != nil {
		return 0, e
	}
	rows, e := tx.Query(ctx, marketHistoryDemandSQL, s.TenantID, at)
	if e != nil {
		return 0, e
	}
	desired := map[string]bool{}
	for rows.Next() {
		var raw []byte
		var first, last time.Time
		var open bool
		var m md.Mapping
		if e = rows.Scan(&raw, &first, &last, &open); e != nil {
			rows.Close()
			return 0, e
		}
		if e = json.Unmarshal(raw, &m); e != nil {
			rows.Close()
			return 0, e
		}
		if !open {
			continue
		}
		for _, p := range m.HistoryPeriods() {
			if p.Provider == s.Provider && p.AccessScope == s.AccessScope && !at.Before(p.From) && (p.To.IsZero() || at.Before(p.To)) {
				desired[p.InstrumentID] = true
			}
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	ids := []string{}
	for id := range desired {
		ids = append(ids, id)
		if _, e = tx.Exec(ctx, `INSERT INTO market_data_subscriptions(tenant_id,provider,access_scope,instrument_id,kind,position_demand) VALUES($1,$2,$3,$4,'daily_candle',true) ON CONFLICT(tenant_id,provider,access_scope,instrument_id,kind) DO UPDATE SET position_demand=true WHERE NOT market_data_subscriptions.position_demand`, s.TenantID, s.Provider, s.AccessScope, id); e != nil {
			return 0, e
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE market_data_subscriptions SET position_demand=false WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND position_demand AND NOT(instrument_id::text=ANY($4::text[]))`, s.TenantID, s.Provider, s.AccessScope, ids); e != nil {
		return 0, e
	}
	rows, e = tx.Query(ctx, `SELECT instrument_id::text FROM market_data_subscriptions WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND kind='daily_candle' AND (position_demand OR requested_until>$4) AND next_due<=$4 ORDER BY position_demand DESC,next_due,instrument_id LIMIT $5 FOR UPDATE`, s.TenantID, s.Provider, s.AccessScope, at, limit)
	if e != nil {
		return 0, e
	}
	ids = nil
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return 0, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	var active, scoped int
	if e = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE provider=$2) FROM market_data_jobs WHERE tenant_id=$1 AND status IN ('pending','running')`, s.TenantID, s.Provider).Scan(&active, &scoped); e != nil {
		return 0, e
	}
	created := 0
	for _, id := range ids {
		if active >= 100 || scoped >= 20 {
			break
		}
		from, to := at.Truncate(24*time.Hour).AddDate(0, 0, -14), at.Truncate(24*time.Hour)
		var exists bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM market_data_jobs WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 AND kind='daily_candle' AND status IN ('pending','running') AND from_at=$5 AND to_at=$6)`, s.TenantID, s.Provider, s.AccessScope, id, from, to).Scan(&exists); e != nil {
			return 0, e
		}
		if !exists {
			if _, e = tx.Exec(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,priority) VALUES($1,$2,$3,$4,'daily_candle',$5,$6,10)`, s.TenantID, s.Provider, s.AccessScope, id, from, to); e != nil {
				return 0, e
			}
			active++
			scoped++
			created++
		}
		if _, e = tx.Exec(ctx, `UPDATE market_data_subscriptions SET next_due=$5 WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 AND kind='daily_candle'`, s.TenantID, s.Provider, s.AccessScope, id, at.Add(6*time.Hour)); e != nil {
			return 0, e
		}
	}
	return created, tx.Commit(ctx)
}
