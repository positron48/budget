package postgres

import (
	"context"

	"github.com/positron48/budget/internal/domain"
)

type TenantRepo struct{ pool *Pool }

func NewTenantRepo(pool *Pool) *TenantRepo { return &TenantRepo{pool: pool} }

func (r *TenantRepo) Create(ctx context.Context, name, slug, defaultCurrency, ownerUserID string) (domain.Tenant, error) {
	var t domain.Tenant
	err := r.pool.DB.QueryRow(ctx,
		`INSERT INTO tenants (name, slug, default_currency_code) VALUES ($1,$2,$3)
         RETURNING id, name, slug, default_currency_code, created_at`,
		name, slug, defaultCurrency,
	).Scan(&t.ID, &t.Name, &t.Slug, &t.DefaultCurrencyCode, &t.CreatedAt)
	if err != nil {
		return domain.Tenant{}, err
	}
	// grant owner role to creator
	if _, err := r.pool.DB.Exec(ctx,
		`INSERT INTO user_tenants (user_id, tenant_id, role, is_default) VALUES ($1,$2,'owner',false) ON CONFLICT DO NOTHING`,
		ownerUserID, t.ID,
	); err != nil {
		return domain.Tenant{}, err
	}
	return t, nil
}

func (r *TenantRepo) ListForUser(ctx context.Context, userID string) ([]domain.TenantMembership, error) {
	rows, err := r.pool.DB.Query(ctx,
		`SELECT t.id, t.name, t.slug, t.default_currency_code, t.created_at, ut.role, ut.is_default
         FROM user_tenants ut
         JOIN tenants t ON t.id = ut.tenant_id
         WHERE ut.user_id=$1
         ORDER BY ut.is_default DESC, t.created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []domain.TenantMembership
	for rows.Next() {
		var tm domain.TenantMembership
		if err := rows.Scan(&tm.Tenant.ID, &tm.Tenant.Name, &tm.Tenant.Slug, &tm.Tenant.DefaultCurrencyCode, &tm.Tenant.CreatedAt, &tm.Role, &tm.IsDefault); err != nil {
			return nil, err
		}
		res = append(res, tm)
	}
	return res, rows.Err()
}

func (r *TenantRepo) GetByID(ctx context.Context, id string) (domain.Tenant, error) {
	var t domain.Tenant
	err := r.pool.DB.QueryRow(ctx, `SELECT id, name, slug, default_currency_code, created_at FROM tenants WHERE id=$1`, id).Scan(&t.ID, &t.Name, &t.Slug, &t.DefaultCurrencyCode, &t.CreatedAt)
	return t, err
}

// HasMembership returns true if user is a member of tenant
func (r *TenantRepo) HasMembership(ctx context.Context, userID, tenantID string) (bool, error) {
	var exists bool
	err := r.pool.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_tenants WHERE user_id=$1 AND tenant_id=$2)`, userID, tenantID).Scan(&exists)
	return exists, err
}
