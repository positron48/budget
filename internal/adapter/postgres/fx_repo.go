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

// UpsertRate inserts or updates a rate and returns the stored row values
func (r *FxRepo) UpsertRate(ctx context.Context, from, to, rateDecimal string, asOf time.Time, provider string) (struct{
    From     string
    To       string
    Rate     string
    AsOf     time.Time
    Provider string
}, error) {
    var row struct{
        From     string
        To       string
        Rate     string
        AsOf     time.Time
        Provider string
    }
    err := r.pool.DB.QueryRow(ctx,
        `INSERT INTO fx_rates (from_currency_code, to_currency_code, rate, as_of, provider)
         VALUES ($1,$2,$3::numeric,$4::date,$5)
         ON CONFLICT (from_currency_code, to_currency_code, as_of, provider)
         DO UPDATE SET rate=EXCLUDED.rate
         RETURNING from_currency_code, to_currency_code, rate::text, as_of, provider`,
        from, to, rateDecimal, asOf.Truncate(24*time.Hour), provider,
    ).Scan(&row.From, &row.To, &row.Rate, &row.AsOf, &row.Provider)
    return row, err
}

// BatchGetRates returns nearest rates for multiple source currencies to the same target as of date
func (r *FxRepo) BatchGetRates(ctx context.Context, fromCurrencies []string, to string, asOf time.Time) ([]struct{
    From     string
    To       string
    Rate     string
    AsOf     time.Time
    Provider string
}, error) {
    rows, err := r.pool.DB.Query(ctx,
        `SELECT DISTINCT ON (from_currency_code) from_currency_code, to_currency_code, rate::text, as_of, provider
         FROM fx_rates
         WHERE from_currency_code = ANY($1) AND to_currency_code=$2 AND as_of <= $3::date
         ORDER BY from_currency_code, as_of DESC`,
        fromCurrencies, to, asOf.Truncate(24*time.Hour),
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []struct{
        From     string
        To       string
        Rate     string
        AsOf     time.Time
        Provider string
    }
    for rows.Next() {
        var row struct{
            From     string
            To       string
            Rate     string
            AsOf     time.Time
            Provider string
        }
        if err := rows.Scan(&row.From, &row.To, &row.Rate, &row.AsOf, &row.Provider); err != nil {
            return nil, err
        }
        out = append(out, row)
    }
    return out, rows.Err()
}
