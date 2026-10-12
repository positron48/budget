package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// A successful explicit refresh keeps only that dataset in demand for 30 days.
// Search does not subscribe a whole set of candidates.
func touchMarketSubscription(ctx context.Context, tx pgx.Tx, s md.Scope, q md.JobRequest) error {
	if q.Kind == "catalog_search" {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO market_data_subscriptions(tenant_id,provider,access_scope,instrument_id,kind,requested_until,next_due) VALUES($1,$2,$3,$4,$5,clock_timestamp()+interval '30 days',clock_timestamp()+CASE WHEN $5='last_price' THEN interval '30 minutes' ELSE interval '6 hours' END) ON CONFLICT(tenant_id,provider,access_scope,instrument_id,kind) DO UPDATE SET requested_until=GREATEST(market_data_subscriptions.requested_until,EXCLUDED.requested_until),next_due=GREATEST(market_data_subscriptions.next_due,EXCLUDED.next_due)`, s.TenantID, s.Provider, s.AccessScope, q.InstrumentID, q.Kind)
	return err
}

// ScheduleMarket refreshes demand from committed security legs and reviewed
// mappings, then puts due requests in the same durable queue as manual loads.
// at is injected for deterministic tests; the worker supplies its UTC clock.
func (r *MarketDataRepo) ScheduleMarket(ctx context.Context, scope md.Scope, at time.Time, limit int) (int, error) {
	if md.ValidateScope(scope) != nil || at.Year() < 1900 || at.Year() > 2200 || limit < 1 || limit > 100 {
		return 0, md.ErrInvalid
	}
	at = at.UTC()
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market-refresh:"+scope.TenantID); err != nil {
		return 0, err
	}
	var enabled bool
	err = tx.QueryRow(ctx, `SELECT enabled FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 FOR SHARE`, scope.TenantID, scope.Provider, scope.AccessScope).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !enabled {
		return 0, tx.Commit(ctx)
	}
	if err != nil {
		return 0, err
	}
	// Sum per ledger first: opposite positions in separate accounts must not
	// cancel demand. Legs already incorporate amendments/cancellations atomically.
	rows, err := tx.Query(ctx, `SELECT m.data FROM investment_market_mappings m WHERE m.tenant_id=$1 AND EXISTS (SELECT 1 FROM investment_security_legs s JOIN investment_ledgers l ON l.tenant_id=s.tenant_id AND l.id=s.ledger_id WHERE s.tenant_id=m.tenant_id AND s.instrument_key=m.instrument_key AND s.effective_at<=$2 AND NOT l.archived GROUP BY s.ledger_id HAVING sum(s.quantity)<>0)`, scope.TenantID, at)
	if err != nil {
		return 0, err
	}
	var mappings []md.Mapping
	for rows.Next() {
		var raw []byte
		var m md.Mapping
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal(raw, &m)
		}
		if err != nil {
			rows.Close()
			return 0, err
		}
		mappings = append(mappings, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	desired := []string{}
	for _, m := range mappings {
		for _, p := range m.Periods {
			if p.Provider != scope.Provider || p.AccessScope != scope.AccessScope || at.Before(p.From) || !p.To.IsZero() && !at.Before(p.To) {
				continue
			}
			kinds := []string{"last_price", "daily_candle"}
			switch p.Kind {
			case "bond":
				kinds = append(kinds, "bond_facts")
			case "share", "etf":
				kinds = append(kinds, "dividend_facts")
			}
			for _, kind := range kinds {
				desired = append(desired, p.InstrumentID+":"+kind)
				if _, err = tx.Exec(ctx, `INSERT INTO market_data_subscriptions(tenant_id,provider,access_scope,instrument_id,kind,position_demand) VALUES($1,$2,$3,$4,$5,true) ON CONFLICT(tenant_id,provider,access_scope,instrument_id,kind) DO UPDATE SET position_demand=true WHERE NOT market_data_subscriptions.position_demand`, scope.TenantID, scope.Provider, scope.AccessScope, p.InstrumentID, kind); err != nil {
					return 0, err
				}
			}
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE market_data_subscriptions SET position_demand=false WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND position_demand AND NOT (instrument_id::text||':'||kind=ANY($4::text[]))`, scope.TenantID, scope.Provider, scope.AccessScope, desired); err != nil {
		return 0, err
	}
	benchmarkPeriods, err := benchmarkMarketPeriods(ctx, tx, scope)
	if err != nil {
		return 0, err
	}
	benchmarkIDs := []string{}
	for _, p := range benchmarkPeriods {
		if at.Before(p.From) || !p.To.IsZero() && !at.Before(p.To) {
			continue
		}
		benchmarkIDs = append(benchmarkIDs, p.InstrumentID)
		if _, err = tx.Exec(ctx, `INSERT INTO market_data_subscriptions(tenant_id,provider,access_scope,instrument_id,kind,benchmark_demand) VALUES($1,$2,$3,$4,'daily_candle',true) ON CONFLICT(tenant_id,provider,access_scope,instrument_id,kind) DO UPDATE SET benchmark_demand=true WHERE NOT market_data_subscriptions.benchmark_demand`, scope.TenantID, scope.Provider, scope.AccessScope, p.InstrumentID); err != nil {
			return 0, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE market_data_subscriptions SET benchmark_demand=false WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND benchmark_demand AND NOT (instrument_id::text=ANY($4::text[]))`, scope.TenantID, scope.Provider, scope.AccessScope, benchmarkIDs); err != nil {
		return 0, err
	}
	fxPolicies, e := fxSeriesPolicies(ctx, tx, scope)
	if e != nil {
		return 0, e
	}
	fxIDs := []string{}
	for _, p := range fxPolicies {
		for _, series := range p.Series {
			if at.Before(series.From) || !series.To.IsZero() && !at.Before(series.To) {
				continue
			}
			fxIDs = append(fxIDs, series.InstrumentID)
			if _, err = tx.Exec(ctx, `INSERT INTO market_data_subscriptions(tenant_id,provider,access_scope,instrument_id,kind,fx_demand) VALUES($1,$2,$3,$4,'daily_candle',true) ON CONFLICT(tenant_id,provider,access_scope,instrument_id,kind) DO UPDATE SET fx_demand=true WHERE NOT market_data_subscriptions.fx_demand`, scope.TenantID, scope.Provider, scope.AccessScope, series.InstrumentID); err != nil {
				return 0, err
			}
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE market_data_subscriptions SET fx_demand=false WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND fx_demand AND NOT (instrument_id::text=ANY($4::text[]))`, scope.TenantID, scope.Provider, scope.AccessScope, fxIDs); err != nil {
		return 0, err
	}
	rows, err = tx.Query(ctx, `SELECT instrument_id::text,kind,(position_demand OR benchmark_demand OR fx_demand) FROM market_data_subscriptions WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND (position_demand OR benchmark_demand OR fx_demand OR requested_until>$4) AND next_due<=$4 ORDER BY (position_demand OR benchmark_demand OR fx_demand) DESC,next_due,instrument_id,kind LIMIT $5 FOR UPDATE`, scope.TenantID, scope.Provider, scope.AccessScope, at, limit)
	if err != nil {
		return 0, err
	}
	var requests []md.JobRequest
	priorities := map[string]int{}
	for rows.Next() {
		var q md.JobRequest
		var position bool
		if err = rows.Scan(&q.InstrumentID, &q.Kind, &position); err != nil {
			rows.Close()
			return 0, err
		}
		priority := 20
		if position {
			switch q.Kind {
			case "last_price":
				priority = 0
			case "daily_candle":
				priority = 10
			case "bond_facts", "dividend_facts":
				priority = 15
			}
		}
		priorities[q.InstrumentID+":"+q.Kind] = priority
		requests = append(requests, q)
		if q.Kind == "dividend_facts" {
			// A future credit can follow a past record date. Refresh both sides
			// without exceeding the provider's bounded 365-day job contract.
			past := q
			past.To = at.Truncate(24 * time.Hour)
			past.From = past.To.Add(-365 * 24 * time.Hour)
			requests = append(requests, past)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	var active int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM market_data_jobs WHERE tenant_id=$1 AND status IN ('pending','running')`, scope.TenantID).Scan(&active); err != nil {
		return 0, err
	}
	count := 0
	for _, q := range requests {
		interval := 30 * time.Minute
		if q.Kind == "dividend_facts" && q.From.IsZero() {
			q.From = at.Truncate(24 * time.Hour)
			q.To = q.From.Add(365*24*time.Hour - time.Second)
		}
		if q.Kind != "last_price" {
			interval = 6 * time.Hour
			if q.From.IsZero() {
				q.From = at.Truncate(24*time.Hour).AddDate(0, 0, -14)
				q.To = at.Truncate(time.Second)
			}
		}
		// Keep bond request within the existing 365-day provider contract.
		if q.Kind == "bond_facts" {
			q.From = at.Truncate(24 * time.Hour)
			q.To = q.From.Add(365*24*time.Hour - time.Second)
		}
		if err = md.ValidateRequest(q); err != nil {
			return 0, err
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM market_data_jobs WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 AND kind=$5 AND status IN ('pending','running') AND (kind='last_price' OR (kind='dividend_facts' AND from_at<=$6 AND to_at>=$7) OR (kind<>'dividend_facts' AND from_at>=$6)))`, scope.TenantID, scope.Provider, scope.AccessScope, q.InstrumentID, q.Kind, q.From, q.To).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			if active >= 100 {
				continue
			}
			if _, err = tx.Exec(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,priority) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,search_query,instrument_kind) WHERE status IN ('pending','running') DO NOTHING`, scope.TenantID, scope.Provider, scope.AccessScope, q.InstrumentID, q.Kind, q.From, q.To, priorities[q.InstrumentID+":"+q.Kind]); err != nil {
				return 0, err
			}
			active++
			count++
		}
		if _, err = tx.Exec(ctx, `UPDATE market_data_subscriptions SET next_due=$6 WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 AND kind=$5`, scope.TenantID, scope.Provider, scope.AccessScope, q.InstrumentID, q.Kind, at.Add(interval)); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
}
