package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

const marketHistoryDemandSQL = `WITH totals AS (
 SELECT s.instrument_key,s.ledger_id,sum(s.quantity) quantity,min(s.effective_at) first_at,max(s.effective_at) last_at
 FROM investment_security_legs s JOIN investment_ledgers l ON l.tenant_id=s.tenant_id AND l.id=s.ledger_id
 WHERE s.tenant_id=$1 AND s.effective_at<=$2 AND NOT l.archived GROUP BY s.instrument_key,s.ledger_id)
 SELECT m.data,min(t.first_at),max(t.last_at),bool_or(t.quantity<>0) FROM totals t
 JOIN investment_market_mappings m ON m.tenant_id=$1 AND m.instrument_key=t.instrument_key
 GROUP BY m.instrument_key,m.data ORDER BY m.instrument_key`

// ScheduleMarketHistory hydrates open and closed positions from committed
// history. The jobs themselves are a durable request checkpoint; no separate
// cursor can falsely turn provider errors into complete market coverage.
// Native transient terminal failures get a new job after a day; their original
// error remains auditable. Legacy broker retry behavior is unchanged.
func (r *MarketDataRepo) ScheduleMarketHistory(ctx context.Context, scope md.Scope, at time.Time, limit int) (int, error) {
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
	rows, err := tx.Query(ctx, marketHistoryDemandSQL, scope.TenantID, at)
	if err != nil {
		return 0, err
	}
	var wants []md.JobRequest
	for rows.Next() {
		var raw []byte
		var m md.Mapping
		var first, last time.Time
		var open bool
		if err = rows.Scan(&raw, &first, &last, &open); err == nil {
			err = json.Unmarshal(raw, &m)
		}
		if err != nil {
			rows.Close()
			return 0, err
		}
		wants = append(wants, md.MappedHistoryRanges(m, scope, first, last, at, open)...)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	benchmarkPeriods, err := benchmarkMarketPeriods(ctx, tx, scope)
	if err != nil {
		return 0, err
	}
	wants = append(wants, benchmarkHistoryRanges(benchmarkPeriods, scope, at)...)
	fxPolicies, e := fxSeriesPolicies(ctx, tx, scope)
	if e != nil {
		return 0, e
	}
	wants = append(wants, fxSeriesHistoryRanges(fxPolicies, at)...)
	var active, scoped int
	if err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE provider=$2) FROM market_data_jobs WHERE tenant_id=$1 AND status IN ('pending','running')`, scope.TenantID, scope.Provider).Scan(&active, &scoped); err != nil {
		return 0, err
	}
	created := 0
	for _, want := range wants {
		budget := min(limit-created, 100-active)
		if scope.Provider != "tinvest" {
			budget = min(budget, 20-scoped)
		}
		if budget <= 0 {
			break
		}
		rows, err = tx.Query(ctx, `SELECT from_at,to_at FROM market_data_jobs WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 AND kind='daily_candle' AND from_at<$6 AND to_at>$5 AND ($2='tinvest' OR status<>'error' OR error_code<>'provider_temporary' OR finished_at IS NULL OR finished_at>$7::timestamptz-interval '24 hours') ORDER BY from_at,to_at`, scope.TenantID, scope.Provider, scope.AccessScope, want.InstrumentID, want.From, want.To, at)
		if err != nil {
			return 0, err
		}
		var requested []md.JobRequest
		for rows.Next() {
			q := md.JobRequest{InstrumentID: want.InstrumentID, Kind: want.Kind}
			if err = rows.Scan(&q.From, &q.To); err != nil {
				rows.Close()
				return 0, err
			}
			requested = append(requested, q)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		gaps, e := md.PlanHistoryGaps(want, requested, budget)
		if e != nil {
			return 0, e
		}
		for _, q := range gaps {
			if err = md.ValidateRequest(q); err != nil {
				return 0, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,priority) VALUES($1,$2,$3,$4,'daily_candle',$5,$6,30) ON CONFLICT(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,search_query,instrument_kind) WHERE status IN ('pending','running') DO NOTHING`, scope.TenantID, scope.Provider, scope.AccessScope, q.InstrumentID, q.From, q.To); err != nil {
				return 0, err
			}
			created++
			active++
			scoped++
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return created, nil
}
