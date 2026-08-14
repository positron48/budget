package postgres

import (
	"context"

	"github.com/positron48/budget/internal/domain"
)

type CurrencyExchangeRepo struct{ pool *Pool }

func NewCurrencyExchangeRepo(pool *Pool) *CurrencyExchangeRepo {
	return &CurrencyExchangeRepo{pool: pool}
}

func (r *CurrencyExchangeRepo) Create(ctx context.Context, exchange domain.CurrencyExchange) (domain.CurrencyExchange, error) {
	var created domain.CurrencyExchange
	var fromDecimalValue, toDecimalValue string
	err := r.pool.DB.QueryRow(ctx, `
		INSERT INTO currency_exchanges (
			tenant_id, user_id, from_amount_numeric, from_currency_code,
			to_amount_numeric, to_currency_code, occurred_at, note
		) VALUES ($1, $2, $3::numeric, $4, $5::numeric, $6, $7, $8)
		RETURNING id, tenant_id, user_id,
			from_amount_numeric::text, from_currency_code,
			to_amount_numeric::text, to_currency_code,
			rate_numeric::text, occurred_at, note, created_at`,
		exchange.TenantID, exchange.UserID,
		toDecimal(exchange.FromAmount.MinorUnits), exchange.FromAmount.CurrencyCode,
		toDecimal(exchange.ToAmount.MinorUnits), exchange.ToAmount.CurrencyCode,
		exchange.OccurredAt, exchange.Note,
	).Scan(
		&created.ID, &created.TenantID, &created.UserID,
		&fromDecimalValue, &created.FromAmount.CurrencyCode,
		&toDecimalValue, &created.ToAmount.CurrencyCode,
		&created.RateDecimal, &created.OccurredAt, &created.Note, &created.CreatedAt,
	)
	if err != nil {
		return domain.CurrencyExchange{}, err
	}
	created.FromAmount.MinorUnits = fromDecimal(fromDecimalValue)
	created.ToAmount.MinorUnits = fromDecimal(toDecimalValue)
	return created, nil
}

func (r *CurrencyExchangeRepo) Update(ctx context.Context, exchange domain.CurrencyExchange) (domain.CurrencyExchange, error) {
	var updated domain.CurrencyExchange
	var fromDecimalValue, toDecimalValue string
	err := r.pool.DB.QueryRow(ctx, `
		UPDATE currency_exchanges SET
			from_amount_numeric = $3::numeric,
			from_currency_code = $4,
			to_amount_numeric = $5::numeric,
			to_currency_code = $6,
			occurred_at = $7,
			note = $8
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, user_id,
			from_amount_numeric::text, from_currency_code,
			to_amount_numeric::text, to_currency_code,
			rate_numeric::text, occurred_at, note, created_at`,
		exchange.TenantID, exchange.ID,
		toDecimal(exchange.FromAmount.MinorUnits), exchange.FromAmount.CurrencyCode,
		toDecimal(exchange.ToAmount.MinorUnits), exchange.ToAmount.CurrencyCode,
		exchange.OccurredAt, exchange.Note,
	).Scan(
		&updated.ID, &updated.TenantID, &updated.UserID,
		&fromDecimalValue, &updated.FromAmount.CurrencyCode,
		&toDecimalValue, &updated.ToAmount.CurrencyCode,
		&updated.RateDecimal, &updated.OccurredAt, &updated.Note, &updated.CreatedAt,
	)
	if err != nil {
		return domain.CurrencyExchange{}, err
	}
	updated.FromAmount.MinorUnits = fromDecimal(fromDecimalValue)
	updated.ToAmount.MinorUnits = fromDecimal(toDecimalValue)
	return updated, nil
}

func (r *CurrencyExchangeRepo) List(ctx context.Context, tenantID string, page, pageSize int) ([]domain.CurrencyExchange, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	var total int64
	if err := r.pool.DB.QueryRow(ctx, `SELECT COUNT(*) FROM currency_exchanges WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.DB.Query(ctx, `
		SELECT id, tenant_id, user_id,
			from_amount_numeric::text, from_currency_code,
			to_amount_numeric::text, to_currency_code,
			rate_numeric::text, occurred_at, note, created_at
		FROM currency_exchanges
		WHERE tenant_id = $1
		ORDER BY occurred_at DESC, created_at DESC
		OFFSET $2 LIMIT $3`, tenantID, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]domain.CurrencyExchange, 0)
	for rows.Next() {
		var item domain.CurrencyExchange
		var fromDecimalValue, toDecimalValue string
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.UserID,
			&fromDecimalValue, &item.FromAmount.CurrencyCode,
			&toDecimalValue, &item.ToAmount.CurrencyCode,
			&item.RateDecimal, &item.OccurredAt, &item.Note, &item.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		item.FromAmount.MinorUnits = fromDecimal(fromDecimalValue)
		item.ToAmount.MinorUnits = fromDecimal(toDecimalValue)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *CurrencyExchangeRepo) Delete(ctx context.Context, tenantID, id string) error {
	_, err := r.pool.DB.Exec(ctx, `DELETE FROM currency_exchanges WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return err
}
