package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"time"
)

func (r *InvestmentImportRepo) BrokerTradeJournalCandidates(ctx context.Context, tenant, ledger string, at time.Time) ([]d.InvestmentOperation, error) {
	var exists bool
	if err := r.pool.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_ledgers WHERE tenant_id=$1 AND id=$2)`, tenant, ledger).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, imp.ErrNotFound
	}
	// Other ledgers are deliberately included, but never other tenants.
	rows, err := r.pool.DB.Query(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND status='posted' AND kind IN ('buy','sell') AND occurred_at >= $2 AND occurred_at <= $3 ORDER BY occurred_at,ledger_id,sequence LIMIT 10001`, tenant, at.Add(-24*time.Hour), at.Add(24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []d.InvestmentOperation{}
	for rows.Next() {
		operation, err := scanInvestmentOperation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, operation)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(result) > 10000 {
		return nil, imp.ErrInvalid
	}
	return result, nil
}

// A current, immutable broker claim proves a different execution. Unlinked or
// subsequently amended journal trades remain reconciliation candidates.
const distinctBrokerCandidatesSQL = `SELECT o.data FROM investment_operations o
WHERE o.tenant_id=$1 AND o.status='posted' AND o.kind IN ('buy','sell')
 AND o.occurred_at >= $2 AND o.occurred_at <= $3
 AND NOT EXISTS (
 SELECT 1 FROM investment_broker_links l
 WHERE l.tenant_id=o.tenant_id AND l.ledger_id=o.ledger_id AND l.operation_id=o.id AND l.operation_version=o.version
 AND EXISTS (SELECT 1 FROM investment_broker_claims c WHERE c.tenant_id=l.tenant_id AND c.link_id=l.id)
 AND NOT EXISTS (SELECT 1 FROM investment_broker_claims c WHERE c.tenant_id=l.tenant_id AND c.link_id=l.id
 AND ('tinvest:' || c.account_id || ':' || c.operation_id)=ANY($4::text[])))
ORDER BY o.occurred_at,o.ledger_id,o.sequence LIMIT 10001`

func brokerDistinctCandidates(ctx context.Context, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, tenant string, at time.Time, identities []string) ([]d.InvestmentOperation, error) {
	rows, e := db.Query(ctx, distinctBrokerCandidatesSQL, tenant, at.Add(-24*time.Hour), at.Add(24*time.Hour), identities)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []d.InvestmentOperation{}
	for rows.Next() {
		op, e := scanInvestmentOperation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, op)
	}
	if len(out) > 10000 {
		return nil, imp.ErrInvalid
	}
	return out, rows.Err()
}
func (r *InvestmentImportRepo) BrokerTradeDistinctCandidates(ctx context.Context, tenant, ledger string, at time.Time, identities []string) ([]d.InvestmentOperation, error) {
	var exists bool
	if e := r.pool.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_ledgers WHERE tenant_id=$1 AND id=$2)`, tenant, ledger).Scan(&exists); e != nil {
		return nil, e
	}
	if !exists {
		return nil, imp.ErrNotFound
	}
	return brokerDistinctCandidates(ctx, r.pool.DB, tenant, at, identities)
}
