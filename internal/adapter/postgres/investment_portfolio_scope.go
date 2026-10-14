package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
)

type PortfolioOperatorScope struct {
	TenantID, ActorID string
	SchemaVersion     int64
	SchemaDirty       bool
	Accounts          []PortfolioOperatorAccount
}
type PortfolioOperatorAccount struct {
	ID, Name                 string
	Version, MetadataVersion int64
	PortfolioTracking        bool
}

// InspectPortfolioScope exposes identities and counters only to the local DB
// operator. It does not read credentials, balances, positions or source files.
func (r *InvestmentRepo) InspectPortfolioScope(ctx context.Context, ledger string) (PortfolioOperatorScope, error) {
	var out PortfolioOperatorScope
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var role string
	e = tx.QueryRow(ctx, `SELECT l.tenant_id,l.created_by,m.role FROM investment_ledgers l JOIN user_tenants m ON m.tenant_id=l.tenant_id AND m.user_id=l.created_by WHERE l.id=$1 AND l.portfolio_tracking`, ledger).Scan(&out.TenantID, &out.ActorID, &role)
	if e != nil {
		return out, e
	}
	if role != "owner" && role != "admin" {
		return out, d.ErrInvestmentForbidden
	}
	if e = tx.QueryRow(ctx, `SELECT version,dirty FROM schema_migrations`).Scan(&out.SchemaVersion, &out.SchemaDirty); e != nil {
		return out, e
	}
	rows, e := tx.Query(ctx, `SELECT id,name,version,metadata_version,portfolio_tracking FROM investment_ledgers WHERE tenant_id=$1 ORDER BY id`, out.TenantID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var a PortfolioOperatorAccount
		if e = rows.Scan(&a.ID, &a.Name, &a.Version, &a.MetadataVersion, &a.PortfolioTracking); e != nil {
			rows.Close()
			return out, e
		}
		out.Accounts = append(out.Accounts, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
