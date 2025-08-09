package postgres

import (
	"context"
	"time"
)

type FxRepo struct{ pool *Pool }

func NewFxRepo(pool *Pool) *FxRepo { return &FxRepo{pool: pool} }

// GetRateAsOf returns rate for exact date or nearest previous date (same provider preference TBD)
func (r *FxRepo) GetRateAsOf(ctx context.Context, from, to string, asOf time.Time) (string, string, error) {
	var rate string
	var provider string
	// exact match
	err := r.pool.DB.QueryRow(ctx, `SELECT rate::text, provider FROM fx_rates WHERE from_currency_code=$1 AND to_currency_code=$2 AND as_of=$3 ORDER BY provider LIMIT 1`, from, to, asOf.Truncate(24*time.Hour)).Scan(&rate, &provider)
	if err == nil {
		return rate, provider, nil
	}
	// nearest previous
	err = r.pool.DB.QueryRow(ctx, `SELECT rate::text, provider FROM fx_rates WHERE from_currency_code=$1 AND to_currency_code=$2 AND as_of<$3 ORDER BY as_of DESC LIMIT 1`, from, to, asOf.Truncate(24*time.Hour)).Scan(&rate, &provider)
	return rate, provider, err
}
