package postgres

import (
    "context"
    "errors"
    "strings"

    pgx "github.com/jackc/pgx/v5"
    "github.com/positron48/budget/internal/domain"
    useauth "github.com/positron48/budget/internal/usecase/auth"
)

type UserRepo struct{ pool *Pool }

func NewUserRepo(pool *Pool) *UserRepo { return &UserRepo{pool: pool} }

type TenantMembership struct {
	TenantID  string
	Role      string
	IsDefault bool
}

type User struct {
	ID           string
	Email        string
	Name         string
	Locale       string
	PasswordHash string
}

func (r *UserRepo) CreateWithDefaultTenant(ctx context.Context, email, passwordHash, name, locale, tenantName string) (useauth.User, useauth.Tenant, error) {
	var u useauth.User
	var t useauth.Tenant
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return useauth.User{}, useauth.Tenant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	email = strings.ToLower(strings.TrimSpace(email))
	if tenantName == "" {
		tenantName = "My Budget"
	}

	// create tenant
	if err := tx.QueryRow(ctx,
		`INSERT INTO tenants (name, default_currency_code) VALUES ($1, $2) RETURNING id, name, default_currency_code`,
		tenantName, "RUB",
	).Scan(&t.ID, &t.Name, &t.DefaultCurrencyCode); err != nil {
		return useauth.User{}, useauth.Tenant{}, err
	}
	// create user
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (email, name, locale, password_hash) VALUES ($1,$2,$3,$4)
         RETURNING id, email, name, locale, password_hash`,
		email, name, locale, passwordHash,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Locale, &u.PasswordHash); err != nil {
		return useauth.User{}, useauth.Tenant{}, err
	}
	// membership default owner
	if _, err := tx.Exec(ctx,
		`INSERT INTO user_tenants (user_id, tenant_id, role, is_default) VALUES ($1,$2,'owner',true)`,
		u.ID, t.ID,
	); err != nil {
		return useauth.User{}, useauth.Tenant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return useauth.User{}, useauth.Tenant{}, err
	}
	return u, t, nil
}

type Tenant struct {
	ID                  string
	Name                string
	DefaultCurrencyCode string
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (useauth.User, []useauth.TenantMembership, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u useauth.User
	err := r.pool.DB.QueryRow(ctx,
		`SELECT id, email, name, locale, password_hash FROM users WHERE email=$1`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Locale, &u.PasswordHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return useauth.User{}, nil, err
		}
		return useauth.User{}, nil, err
	}
	rows, err := r.pool.DB.Query(ctx,
		`SELECT tenant_id, role, is_default FROM user_tenants WHERE user_id=$1`, u.ID,
	)
	if err != nil {
		return useauth.User{}, nil, err
	}
	defer rows.Close()
	var ms []useauth.TenantMembership
	for rows.Next() {
		var m useauth.TenantMembership
		if err := rows.Scan(&m.TenantID, &m.Role, &m.IsDefault); err != nil {
			return useauth.User{}, nil, err
		}
		ms = append(ms, m)
	}
	return u, ms, rows.Err()
}

// Additional methods for UserService usecase
func (r *UserRepo) GetByID(ctx context.Context, id string) (domain.User, error) {
    var u domain.User
    err := r.pool.DB.QueryRow(ctx,
        `SELECT id, email, name, locale, password_hash, email_verified, created_at, updated_at FROM users WHERE id=$1`, id,
    ).Scan(&u.ID, &u.Email, &u.Name, &u.Locale, &u.PasswordHash, &u.EmailVerified, &u.CreatedAt, &u.UpdatedAt)
    return u, err
}

func (r *UserRepo) UpdateProfile(ctx context.Context, id, name, locale string) (domain.User, error) {
    _, err := r.pool.DB.Exec(ctx,
        `UPDATE users SET name=$2, locale=$3, updated_at=now() WHERE id=$1`, id, name, locale,
    )
    if err != nil {
        return domain.User{}, err
    }
    return r.GetByID(ctx, id)
}

func (r *UserRepo) ChangePassword(ctx context.Context, id, newHash string) error {
    _, err := r.pool.DB.Exec(ctx,
        `UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, id, newHash,
    )
    return err
}
