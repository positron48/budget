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
         RETURNING id, name, COALESCE(slug, ''), default_currency_code, created_at`,
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
		`SELECT t.id, t.name, COALESCE(t.slug, ''), t.default_currency_code, t.created_at, ut.role, ut.is_default
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
	err := r.pool.DB.QueryRow(ctx, `SELECT id, name, COALESCE(slug, ''), default_currency_code, created_at FROM tenants WHERE id=$1`, id).Scan(&t.ID, &t.Name, &t.Slug, &t.DefaultCurrencyCode, &t.CreatedAt)
	return t, err
}

// HasMembership returns true if user is a member of tenant
func (r *TenantRepo) HasMembership(ctx context.Context, userID, tenantID string) (bool, error) {
	var exists bool
	err := r.pool.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_tenants WHERE user_id=$1 AND tenant_id=$2)`, userID, tenantID).Scan(&exists)
	return exists, err
}

// UpdateTenant updates basic tenant fields
func (r *TenantRepo) UpdateTenant(ctx context.Context, tenantID, name, slug, defaultCurrency string) (domain.Tenant, error) {
	_, err := r.pool.DB.Exec(ctx,
		`UPDATE tenants SET name=COALESCE(NULLIF($2,''), name), slug=CASE WHEN $3='' THEN NULL ELSE $3 END, default_currency_code=COALESCE(NULLIF($4,''), default_currency_code)
         WHERE id=$1`, tenantID, name, slug, defaultCurrency,
	)
	if err != nil {
		return domain.Tenant{}, err
	}
	return r.GetByID(ctx, tenantID)
}

// ListMembers returns all memberships with roles
func (r *TenantRepo) ListMembers(ctx context.Context, tenantID string) ([]domain.TenantMembership, error) {
	rows, err := r.pool.DB.Query(ctx,
		`SELECT t.id, t.name, COALESCE(t.slug, ''), t.default_currency_code, t.created_at, ut.role, ut.is_default
         FROM user_tenants ut JOIN tenants t ON t.id = ut.tenant_id WHERE ut.tenant_id=$1 ORDER BY ut.role, t.created_at`, tenantID)
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

// AddMember adds an existing user by email to a tenant with a role
func (r *TenantRepo) AddMember(ctx context.Context, tenantID, userEmail string, role domain.TenantRole) (domain.TenantMembership, error) {
	var userID string
	if err := r.pool.DB.QueryRow(ctx, `SELECT id FROM users WHERE lower(email)=lower($1)`, userEmail).Scan(&userID); err != nil {
		return domain.TenantMembership{}, err
	}
	if _, err := r.pool.DB.Exec(ctx, `INSERT INTO user_tenants (user_id, tenant_id, role) VALUES ($1,$2,$3) ON CONFLICT (user_id, tenant_id) DO UPDATE SET role=EXCLUDED.role`, userID, tenantID, string(role)); err != nil {
		return domain.TenantMembership{}, err
	}
	return r.getMembership(ctx, tenantID, userID)
}

// UpdateMemberRole changes an existing member's role
func (r *TenantRepo) UpdateMemberRole(ctx context.Context, tenantID, userID string, role domain.TenantRole) (domain.TenantMembership, error) {
	if _, err := r.pool.DB.Exec(ctx, `UPDATE user_tenants SET role=$3 WHERE tenant_id=$1 AND user_id=$2`, tenantID, userID, string(role)); err != nil {
		return domain.TenantMembership{}, err
	}
	return r.getMembership(ctx, tenantID, userID)
}

// RemoveMember removes a user from a tenant
func (r *TenantRepo) RemoveMember(ctx context.Context, tenantID, userID string) error {
	_, err := r.pool.DB.Exec(ctx, `DELETE FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, tenantID, userID)
	return err
}

// GetUserRole returns role of a user in a tenant (empty if none)
func (r *TenantRepo) GetUserRole(ctx context.Context, tenantID, userID string) (domain.TenantRole, error) {
	var role string
	err := r.pool.DB.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, tenantID, userID).Scan(&role)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", nil
		}
		return "", err
	}
	return domain.TenantRole(role), nil
}

func (r *TenantRepo) getMembership(ctx context.Context, tenantID, userID string) (domain.TenantMembership, error) {
	var tm domain.TenantMembership
	err := r.pool.DB.QueryRow(ctx,
		`SELECT t.id, t.name, COALESCE(t.slug, ''), t.default_currency_code, t.created_at, ut.role, ut.is_default
         FROM user_tenants ut JOIN tenants t ON t.id = ut.tenant_id WHERE ut.tenant_id=$1 AND ut.user_id=$2`, tenantID, userID,
	).Scan(&tm.Tenant.ID, &tm.Tenant.Name, &tm.Tenant.Slug, &tm.Tenant.DefaultCurrencyCode, &tm.Tenant.CreatedAt, &tm.Role, &tm.IsDefault)
	return tm, err
}
