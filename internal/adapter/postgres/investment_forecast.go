package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func forecastReadAccess(ctx context.Context, tx pgx.Tx, t, u string) error {
	var allowed bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 AND role IN ('owner','admin','member'))`, t, u).Scan(&allowed)
	if e != nil {
		return e
	}
	if !allowed {
		return d.ErrInvestmentForbidden
	}
	return nil
}

func readForecastCatalog(ctx context.Context, tx pgx.Tx, tenant string) ([]inv.ForecastEvent, []inv.ForecastLink, error) {
	events := []inv.ForecastEvent{}
	links := []inv.ForecastLink{}
	for _, name := range []string{"investment_payout_events", "investment_payout_links"} {
		limit := 10001
		if name == "investment_payout_links" {
			limit = inv.MaxPayoutOperations + 1
		}
		rows, e := tx.Query(ctx, `SELECT data FROM `+pgx.Identifier{name}.Sanitize()+` WHERE tenant_id=$1 ORDER BY 1 LIMIT $2`, tenant, limit)
		if e != nil {
			return nil, nil, e
		}
		count := 0
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				break
			}
			count++
			if name == "investment_payout_events" {
				var v inv.ForecastEvent
				e = json.Unmarshal(raw, &v)
				events = append(events, v)
			} else {
				var v inv.ForecastLink
				e = json.Unmarshal(raw, &v)
				links = append(links, v)
			}
			if e != nil {
				break
			}
		}
		rowErr := rows.Err()
		rows.Close()
		if e != nil {
			return nil, nil, e
		}
		if rowErr != nil {
			return nil, nil, rowErr
		}
		if count >= limit {
			return nil, nil, d.ErrInvestmentLimit
		}
	}
	return events, links, nil
}

func (r *InvestmentRepo) ForecastCatalog(ctx context.Context, t, u string) ([]inv.ForecastEvent, []inv.ForecastLink, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = forecastReadAccess(ctx, tx, t, u); e != nil {
		return nil, nil, e
	}
	events, links, e := readForecastCatalog(ctx, tx, t)
	if e != nil {
		return nil, nil, e
	}
	return events, links, tx.Commit(ctx)
}

// All projections, definitions, actual links and FX versions belong to one
// repeatable-read snapshot. Market evidence is read, never scheduled or guessed.
func (r *InvestmentRepo) Forecast(ctx context.Context, t, u string, q inv.ForecastQuery) (inv.ForecastReport, error) {
	var out inv.ForecastReport
	q, e := inv.NormalizeForecastQuery(q)
	if e != nil {
		return out, e
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = forecastReadAccess(ctx, tx, t, u); e != nil {
		return out, e
	}
	rows, e := tx.Query(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 ORDER BY id LIMIT 101`, t)
	if e != nil {
		return out, e
	}
	ledgers := []d.InvestmentLedger{}
	for rows.Next() {
		v, err := scanLedger(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		ledgers = append(ledgers, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	operations := []d.InvestmentOperation{}
	rows, e = tx.Query(ctx, forecastOperationsSQL, t, q.LedgerIDs, inv.MaxPayoutOperations+1)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		v, err := scanInvestmentOperation(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		operations = append(operations, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	legs := []inv.ForecastSecurityLeg{}
	rows, e = tx.Query(ctx, forecastLegsSQL, t, q.LedgerIDs, inv.MaxPayoutOperations*4+1)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v inv.ForecastSecurityLeg
		if e = rows.Scan(&v.LedgerID, &v.OperationID, &v.InstrumentKey, &v.Quantity, &v.EffectiveAt); e != nil {
			rows.Close()
			return out, e
		}
		legs = append(legs, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	events, links, e := readForecastCatalog(ctx, tx, t)
	if e != nil {
		return out, e
	}
	at := time.Now().UTC()
	var fixings []calc.FXFixing
	if q.FXPolicy != "" {
		policy, points, err := readFXSnapshot(ctx, tx, t, q.FXPolicy, at, time.Time{})
		if err != nil {
			return out, err
		}
		if policy.Version > 0 {
			q.FXPolicy = md.FXPolicyRef(policy.Key, policy.Version)
			for _, point := range points {
				fixings = append(fixings, point.Fixing())
			}
		}
	}
	out, e = inv.BuildForecast(t, ledgers, operations, legs, events, links, q, at, fixings)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

const forecastOperationsSQL = `SELECT data FROM investment_operations WHERE tenant_id=$1 AND (cardinality($2::uuid[])=0 OR ledger_id=ANY($2::uuid[])) ORDER BY ledger_id,sequence LIMIT $3`
const forecastLegsSQL = `SELECT ledger_id::text,operation_id::text,instrument_key,quantity::text,effective_at FROM investment_security_legs WHERE tenant_id=$1 AND (cardinality($2::uuid[])=0 OR ledger_id=ANY($2::uuid[])) ORDER BY ledger_id,operation_id,ordinal LIMIT $3`

// The tenant financial lock also serializes archive restore, so definitions
// cannot slip into a supposedly empty destination while it is being restored.
func (r *InvestmentRepo) forecastWriteTx(ctx context.Context, t, u string) (pgx.Tx, error) {
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	if e = lockInvestment(ctx, tx, t); e == nil {
		e = groupAccess(ctx, tx, t, u, true)
	}
	if e != nil {
		_ = tx.Rollback(ctx)
		return nil, e
	}
	return tx, nil
}

func (r *InvestmentRepo) SaveForecastEvent(ctx context.Context, t, u string, event inv.ForecastEvent, expected int64) (inv.ForecastEvent, error) {
	event, e := inv.NormalizeForecastEvent(event)
	if e != nil || expected < 0 || expected == 1<<63-1 {
		return event, d.ErrInvestmentInvalid
	}
	event.Version = expected + 1
	tx, e := r.forecastWriteTx(ctx, t, u)
	if e != nil {
		return event, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	raw, e := json.Marshal(event)
	if e != nil {
		return event, e
	}
	hash := investmentHash(struct {
		Event inv.ForecastEvent
		Actor string
	}{event, u})
	var version int64
	var previousHash string
	e = tx.QueryRow(ctx, `SELECT version,payload_hash FROM investment_payout_events WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, event.ID).Scan(&version, &previousHash)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return event, e
	}
	if version != expected {
		if version == event.Version && previousHash == hash {
			return event, tx.Commit(ctx)
		}
		return event, d.ErrInvestmentConflict
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_payout_events(tenant_id,id,version,data,payload_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,id) DO UPDATE SET version=EXCLUDED.version,data=EXCLUDED.data,payload_hash=EXCLUDED.payload_hash`, t, event.ID, event.Version, raw, hash); e != nil {
		return event, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_payout_event_revisions(tenant_id,event_id,version,data,actor_id,reason) VALUES($1,$2,$3,$4,$5,$6)`, t, event.ID, event.Version, raw, u, event.Reason); e != nil {
		return event, e
	}
	return event, tx.Commit(ctx)
}

func (r *InvestmentRepo) SaveForecastLink(ctx context.Context, t, u string, link inv.ForecastLink, expected int64) (inv.ForecastLink, error) {
	link, e := inv.NormalizeForecastLink(link)
	if e != nil || expected < 0 || expected == 1<<63-1 {
		return link, d.ErrInvestmentInvalid
	}
	link.Version = expected + 1
	tx, e := r.forecastWriteTx(ctx, t, u)
	if e != nil {
		return link, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	raw, e := json.Marshal(link)
	if e != nil {
		return link, e
	}
	hash := investmentHash(struct {
		Link  inv.ForecastLink
		Actor string
	}{link, u})
	var version int64
	var oldHash string
	e = tx.QueryRow(ctx, `SELECT version,payload_hash FROM investment_payout_links WHERE tenant_id=$1 AND operation_id=$2 FOR UPDATE`, t, link.OperationID).Scan(&version, &oldHash)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return link, e
	}
	if version != expected {
		if version == link.Version && hash == oldHash {
			return link, tx.Commit(ctx)
		}
		return link, d.ErrInvestmentConflict
	}
	var eventRaw, operationRaw []byte
	if e = tx.QueryRow(ctx, `SELECT data FROM investment_payout_events WHERE tenant_id=$1 AND id=$2 FOR SHARE`, t, link.EventID).Scan(&eventRaw); errors.Is(e, pgx.ErrNoRows) {
		return link, d.ErrInvestmentNotFound
	}
	if e != nil {
		return link, e
	}
	if e = tx.QueryRow(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2 AND id=$3 FOR SHARE`, t, link.LedgerID, link.OperationID).Scan(&operationRaw); errors.Is(e, pgx.ErrNoRows) {
		return link, d.ErrInvestmentNotFound
	}
	if e != nil {
		return link, e
	}
	var event inv.ForecastEvent
	var operation d.InvestmentOperation
	if e = json.Unmarshal(eventRaw, &event); e != nil {
		return link, e
	}
	if e = json.Unmarshal(operationRaw, &operation); e != nil {
		return link, e
	}
	if !link.Cancelled && (event.Cancelled || event.Version != link.EventVersion || operation.Version != link.OperationVersion || operation.Status != "posted" || operation.Kind != event.Kind || operation.InstrumentKey != event.InstrumentKey || operation.Currency != event.Currency || operation.OccurredAt.After(time.Now())) {
		return link, d.ErrInvestmentConflict
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_payout_links(tenant_id,operation_id,ledger_id,event_id,version,data,payload_hash) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,operation_id) DO UPDATE SET ledger_id=EXCLUDED.ledger_id,event_id=EXCLUDED.event_id,version=EXCLUDED.version,data=EXCLUDED.data,payload_hash=EXCLUDED.payload_hash`, t, link.OperationID, link.LedgerID, link.EventID, link.Version, raw, hash); e != nil {
		return link, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_payout_link_revisions(tenant_id,operation_id,version,data,actor_id,reason) VALUES($1,$2,$3,$4,$5,$6)`, t, link.OperationID, link.Version, raw, u, link.Reason); e != nil {
		return link, e
	}
	return link, tx.Commit(ctx)
}
