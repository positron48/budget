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
