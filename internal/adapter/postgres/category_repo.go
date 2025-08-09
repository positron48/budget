package postgres

import (
	"context"

	"github.com/positron48/budget/internal/domain"
)

type CategoryRepo struct{ pool *Pool }

func NewCategoryRepo(pool *Pool) *CategoryRepo { return &CategoryRepo{pool: pool} }

func (r *CategoryRepo) Create(ctx context.Context, tenantID string, kind domain.CategoryKind, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) {
	var c domain.Category
	err := r.pool.DB.QueryRow(ctx,
		`INSERT INTO categories (tenant_id, kind, code, parent_id, is_active) VALUES ($1,$2,$3,$4,$5)
         RETURNING id, tenant_id, kind, code, parent_id, is_active, created_at`,
		tenantID, string(kind), code, parentID, isActive,
	).Scan(&c.ID, &c.TenantID, &c.Kind, &c.Code, &c.ParentID, &c.IsActive, &c.CreatedAt)
	if err != nil {
		return domain.Category{}, err
	}
	// translations
	for _, tr := range translations {
		if _, err := r.pool.DB.Exec(ctx,
			`INSERT INTO category_i18n (category_id, locale, name, description) VALUES ($1,$2,$3,$4)
             ON CONFLICT (category_id, locale) DO UPDATE SET name=EXCLUDED.name, description=EXCLUDED.description`,
			c.ID, tr.Locale, tr.Name, tr.Description,
		); err != nil {
			return domain.Category{}, err
		}
	}
	c.Translations = translations
	return c, nil
}

func (r *CategoryRepo) Update(ctx context.Context, id string, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) {
	_, err := r.pool.DB.Exec(ctx,
		`UPDATE categories SET code=$2, parent_id=$3, is_active=$4 WHERE id=$1`, id, code, parentID, isActive,
	)
	if err != nil {
		return domain.Category{}, err
	}
	for _, tr := range translations {
		if _, err := r.pool.DB.Exec(ctx,
			`INSERT INTO category_i18n (category_id, locale, name, description) VALUES ($1,$2,$3,$4)
             ON CONFLICT (category_id, locale) DO UPDATE SET name=EXCLUDED.name, description=EXCLUDED.description`,
			id, tr.Locale, tr.Name, tr.Description,
		); err != nil {
			return domain.Category{}, err
		}
	}
	return r.Get(ctx, id)
}

func (r *CategoryRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.DB.Exec(ctx, `DELETE FROM categories WHERE id=$1`, id)
	return err
}

func (r *CategoryRepo) Get(ctx context.Context, id string) (domain.Category, error) {
	var c domain.Category
	err := r.pool.DB.QueryRow(ctx,
		`SELECT id, tenant_id, kind, code, parent_id, is_active, created_at FROM categories WHERE id=$1`, id,
	).Scan(&c.ID, &c.TenantID, &c.Kind, &c.Code, &c.ParentID, &c.IsActive, &c.CreatedAt)
	if err != nil {
		return domain.Category{}, err
	}
	rows, err := r.pool.DB.Query(ctx, `SELECT locale, name, description FROM category_i18n WHERE category_id=$1`, id)
	if err != nil {
		return domain.Category{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var tr domain.CategoryTranslation
		if err := rows.Scan(&tr.Locale, &tr.Name, &tr.Description); err != nil {
			return domain.Category{}, err
		}
		c.Translations = append(c.Translations, tr)
	}
	return c, rows.Err()
}

func (r *CategoryRepo) List(ctx context.Context, tenantID string, kind domain.CategoryKind, includeInactive bool) ([]domain.Category, error) {
	query := `SELECT id, tenant_id, kind, code, parent_id, is_active, created_at FROM categories WHERE tenant_id=$1 AND kind=$2`
	args := []any{tenantID, string(kind)}
	if !includeInactive {
		query += ` AND is_active=true`
	}
	query += ` ORDER BY code`
	rows, err := r.pool.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Category
	for rows.Next() {
		var c domain.Category
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Kind, &c.Code, &c.ParentID, &c.IsActive, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
