package postgres

import (
	"context"
	"errors"
	"strings"

	pgx "github.com/jackc/pgx/v5"
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

func (r *UserRepo) CreateWithDefaultTenant(ctx context.Context, email, passwordHash, name, locale, tenantName string) (User, Tenant, error) {
	var u User
	var t Tenant
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return User{}, Tenant{}, err
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
		return User{}, Tenant{}, err
	}
	// create user
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (email, name, locale, password_hash) VALUES ($1,$2,$3,$4)
         RETURNING id, email, name, locale, password_hash`,
		email, name, locale, passwordHash,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Locale, &u.PasswordHash); err != nil {
		return User{}, Tenant{}, err
	}
	// membership default owner
	if _, err := tx.Exec(ctx,
		`INSERT INTO user_tenants (user_id, tenant_id, role, is_default) VALUES ($1,$2,'owner',true)`,
		u.ID, t.ID,
	); err != nil {
		return User{}, Tenant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, Tenant{}, err
	}
	return u, t, nil
}

type Tenant struct {
	ID                  string
	Name                string
	DefaultCurrencyCode string
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (User, []TenantMembership, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u User
	err := r.pool.DB.QueryRow(ctx,
		`SELECT id, email, name, locale, password_hash FROM users WHERE email=$1`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Locale, &u.PasswordHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, nil, err
		}
		return User{}, nil, err
	}
	rows, err := r.pool.DB.Query(ctx,
		`SELECT tenant_id, role, is_default FROM user_tenants WHERE user_id=$1`, u.ID,
	)
	if err != nil {
		return User{}, nil, err
	}
	defer rows.Close()
	var ms []TenantMembership
	for rows.Next() {
		var m TenantMembership
		if err := rows.Scan(&m.TenantID, &m.Role, &m.IsDefault); err != nil {
			return User{}, nil, err
		}
		ms = append(ms, m)
	}
	return u, ms, rows.Err()
}
