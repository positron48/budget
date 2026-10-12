package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"time"
)

func (r *InvestmentRepo) Payouts(ctx context.Context, t, u string, q inv.PayoutQuery) (inv.PayoutReport, error) {
	var out inv.PayoutReport
	if q.LedgerIDs == nil {
		q.LedgerIDs = []string{}
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var allowed bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 AND role IN ('owner','admin','member'))`, t, u).Scan(&allowed); e != nil {
		return out, e
	}
	if !allowed {
		return out, d.ErrInvestmentForbidden
	}
	rows, e := tx.Query(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 ORDER BY id LIMIT 101`, t)
	if e != nil {
		return out, e
	}
	ledgers := []d.InvestmentLedger{}
	for rows.Next() {
		l, err := scanLedger(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		ledgers = append(ledgers, l)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(ledgers) > 100 {
		return out, d.ErrInvestmentLimit
	}
	// Load only payment facts and embedded commissions, bounded over the entire
	// selected portfolio. Totals and page rows share this repeatable-read snapshot.
	rows, e = tx.Query(ctx, investmentPayoutOperationsSQL, t, q.LedgerIDs, inv.MaxPayoutOperations+1)
	if e != nil {
		return out, e
	}
	ops := []d.InvestmentOperation{}
	for rows.Next() {
		o, err := scanInvestmentOperation(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		ops = append(ops, o)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	out, e = inv.BuildPayoutReport(t, ledgers, ops, q, time.Now())
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

const investmentPayoutOperationsSQL = `SELECT data FROM investment_operations WHERE tenant_id=$1 AND (cardinality($2::uuid[])=0 OR ledger_id=ANY($2::uuid[])) AND status='posted' AND (kind IN ('dividend','coupon','repo_income','cash_interest','fee','tax','tax_refund','amortization','redemption') OR (kind IN ('buy','sell','fx') AND (COALESCE(NULLIF(data->>'Fee',''),'0')::numeric <> 0 OR data ? 'TradeFinancials'))) ORDER BY ledger_id,sequence LIMIT $3`
