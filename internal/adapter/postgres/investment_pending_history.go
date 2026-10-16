package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

const pendingPortfolioTradesSQL = `SELECT EXISTS (
 SELECT 1 FROM investment_import_rows r
 JOIN investment_import_ledger_bindings b ON b.tenant_id=r.tenant_id AND b.import_id=r.import_id AND b.account_id=r.account_id
 WHERE r.tenant_id=$1 AND b.ledger_id=$2
 AND r.source #>> '{raw,Event}' IN ('BUY','SELL','REPAYMENT')
 AND r.source #>> '{raw,Date}' <= $3
 AND NOT EXISTS (SELECT 1 FROM investment_source_links l WHERE l.tenant_id=r.tenant_id AND l.import_id=r.import_id AND l.record=r.record)
)`

func pendingPortfolioTrades(ctx context.Context, tx pgx.Tx, tenant, ledger string, at time.Time) (bool, error) {
	loc, e := time.LoadLocation("Europe/Moscow")
	if e != nil {
		return false, e
	}
	var pending bool
	e = tx.QueryRow(ctx, pendingPortfolioTradesSQL, tenant, ledger, at.In(loc).Format("2006-01-02 15:04:05")).Scan(&pending)
	return pending, e
}

// A pending source trade remains pending from its source timestamp onward.
// MIN on the same source string preserves the existing lexical comparison,
// including its treatment of missing dates, without 31 queries per account.
func readPendingTrackerTradeDates(ctx context.Context, tx pgx.Tx, tenant string, ledgers []string) (map[string]string, error) {
	rows, e := tx.Query(ctx, `SELECT b.ledger_id,min(r.source #>> '{raw,Date}')
 FROM investment_import_rows r
 JOIN investment_import_ledger_bindings b ON b.tenant_id=r.tenant_id AND b.import_id=r.import_id AND b.account_id=r.account_id
 WHERE r.tenant_id=$1 AND b.ledger_id=ANY($2::uuid[])
 AND r.source #>> '{raw,Event}' IN ('BUY','SELL','REPAYMENT')
 AND r.source #>> '{raw,Date}' IS NOT NULL
 AND NOT EXISTS (SELECT 1 FROM investment_source_links l WHERE l.tenant_id=r.tenant_id AND l.import_id=r.import_id AND l.record=r.record)
 GROUP BY b.ledger_id`, tenant, ledgers)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, date string
		if e = rows.Scan(&id, &date); e != nil {
			return nil, e
		}
		out[id] = date
	}
	return out, rows.Err()
}

// Resolve pending source symbols through already posted rows of the same import.
// An unknown or ambiguous identity still affects every selection: guessing a
// ticker-to-instrument mapping could hide a genuinely incomplete category.
const selectedPendingTrackerTradesSQL = `WITH pending AS (
 SELECT b.ledger_id,r.import_id,r.source #>> '{raw,Symbol}' AS symbol,
 r.source #>> '{raw,Exchange}' AS exchange,min(r.source #>> '{raw,Date}') AS date
 FROM investment_import_rows r
 JOIN investment_import_ledger_bindings b ON b.tenant_id=r.tenant_id AND b.import_id=r.import_id AND b.account_id=r.account_id
 WHERE r.tenant_id=$1 AND b.ledger_id=ANY($2::uuid[])
 AND r.source #>> '{raw,Event}' IN ('BUY','SELL','REPAYMENT')
 AND r.source #>> '{raw,Date}' IS NOT NULL
 AND NOT EXISTS (SELECT 1 FROM investment_source_links l WHERE l.tenant_id=r.tenant_id AND l.import_id=r.import_id AND l.record=r.record)
 GROUP BY b.ledger_id,r.import_id,r.source #>> '{raw,Symbol}',r.source #>> '{raw,Exchange}'
), identities AS (
 SELECT r.import_id,r.source #>> '{raw,Symbol}' AS symbol,r.source #>> '{raw,Exchange}' AS exchange,
 CASE WHEN count(DISTINCT o.data->>'InstrumentKey')=1 THEN min(o.data->>'InstrumentKey') ELSE '' END AS key
 FROM investment_import_rows r
 JOIN investment_source_links l ON l.tenant_id=r.tenant_id AND l.import_id=r.import_id AND l.record=r.record
 JOIN investment_operations o ON o.tenant_id=l.tenant_id AND o.ledger_id=l.ledger_id AND o.id=l.operation_id
 WHERE r.tenant_id=$1 AND coalesce(o.data->>'InstrumentKey','')<>''
 GROUP BY r.import_id,r.source #>> '{raw,Symbol}',r.source #>> '{raw,Exchange}'
)
SELECT p.ledger_id,p.date,coalesce(i.key,'') FROM pending p
LEFT JOIN identities i ON i.import_id=p.import_id AND i.symbol=p.symbol AND i.exchange=p.exchange`

func readSelectedPendingTrackerTradeDates(ctx context.Context, tx pgx.Tx, tenant string, ledgers []string, selected func(string, string) bool) (map[string]string, error) {
	rows, e := tx.Query(ctx, selectedPendingTrackerTradesSQL, tenant, ledgers)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var ledger, date, key string
		if e = rows.Scan(&ledger, &date, &key); e != nil {
			return nil, e
		}
		if key != "" && !selected(ledger, key) {
			continue
		}
		if old, ok := out[ledger]; !ok || date < old {
			out[ledger] = date
		}
	}
	return out, rows.Err()
}
