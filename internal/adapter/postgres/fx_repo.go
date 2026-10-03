package postgres

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/usecase/fximport"
)

type FxRepo struct{ pool *Pool }

func (r *FxRepo) GetAssetRates(ctx context.Context, codes []string, target string, at time.Time) ([]domain.AssetFxRate, error) {
	rows, err := r.BatchGetRates(ctx, codes, target, at)
	if err != nil {
		return nil, err
	}
	out := []domain.AssetFxRate{}
	for _, row := range rows {
		out = append(out, domain.AssetFxRate{From: row.From, To: row.To, RateDecimal: row.Rate, AsOf: row.AsOf, Provider: row.Provider})
	}
	return out, nil
}

func NewFxRepo(pool *Pool) *FxRepo { return &FxRepo{pool: pool} }

// GetRateAsOf uses the same date/provider selection as overview and batch reads.
func (r *FxRepo) GetRateAsOf(ctx context.Context, from, to string, asOf time.Time) (string, string, error) {
	rows, err := r.BatchGetRates(ctx, []string{from}, to, asOf)
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 {
		return "", "", pgx.ErrNoRows
	}
	return rows[0].Rate, rows[0].Provider, nil
}

// SaveCBRRates atomically publishes a validated import. Manual entries are untouched.
func (r *FxRepo) SaveCBRRates(ctx context.Context, quotes []fximport.Quote) error {
	codes, rates, dates := []string{}, []string{}, []time.Time{}
	seen := map[string]bool{}
	for _, q := range quotes {
		rate, ok := new(big.Rat).SetString(q.Rate)
		if len(q.Currency) != 3 || q.Currency == "RUB" || q.Date.IsZero() || !ok || rate.Sign() <= 0 {
			return fmt.Errorf("invalid CBR quote")
		}
		key := q.Currency + q.Date.Format(time.DateOnly)
		if seen[key] {
			return fmt.Errorf("duplicate CBR quote %s", key)
		}
		seen[key] = true
		codes = append(codes, q.Currency)
		rates = append(rates, q.Rate)
		dates = append(dates, q.Date)
	}
	_, err := r.pool.DB.Exec(ctx, `INSERT INTO fx_rates(from_currency_code,to_currency_code,rate,as_of,provider)
 SELECT code,'RUB',rate::numeric,day,'cbr' FROM unnest($1::text[],$2::text[],$3::date[]) AS q(code,rate,day)
 ON CONFLICT(from_currency_code,to_currency_code,as_of,provider) DO UPDATE SET rate=EXCLUDED.rate
 WHERE fx_rates.rate IS DISTINCT FROM EXCLUDED.rate`, codes, rates, dates)
	return err
}

// UpsertRate inserts or updates a rate and returns the stored row values
func (r *FxRepo) UpsertRate(ctx context.Context, from, to, rateDecimal string, asOf time.Time, provider string) (struct {
	From     string
	To       string
	Rate     string
	AsOf     time.Time
	Provider string
}, error,
) {
	var row struct {
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
		from, to, rateDecimal, fximport.MoscowDate(asOf), provider,
	).Scan(&row.From, &row.To, &row.Rate, &row.AsOf, &row.Provider)
	return row, err
}

// BatchGetRates returns nearest rates for multiple source currencies to the same target as of date
func (r *FxRepo) BatchGetRates(ctx context.Context, fromCurrencies []string, to string, asOf time.Time) ([]struct {
	From     string
	To       string
	Rate     string
	AsOf     time.Time
	Provider string
}, error,
) {
	rows, err := r.pool.DB.Query(ctx,
		`WITH bases AS (
 SELECT DISTINCT ON (from_currency_code) from_currency_code AS code, rate, as_of
 FROM fx_rates WHERE provider='cbr' AND to_currency_code='RUB' AND as_of <= $3::date
 AND (from_currency_code = ANY($1) OR from_currency_code=$2) AND rate > 0
 ORDER BY from_currency_code, as_of DESC
), rub_bases AS (
 SELECT * FROM bases UNION ALL SELECT 'RUB',1::numeric,$3::date
), candidates AS (
 SELECT from_currency_code,to_currency_code,rate,as_of,provider
 FROM fx_rates WHERE from_currency_code=ANY($1) AND to_currency_code=$2 AND as_of <= $3::date AND rate > 0
 UNION ALL
 SELECT a.code,$2,round(a.rate/b.rate,8),LEAST(a.as_of,b.as_of),'cbr'
 FROM rub_bases a CROSS JOIN rub_bases b WHERE a.code=ANY($1) AND b.code=$2
 UNION ALL
 SELECT $2,$2,1::numeric,$3::date,'identity' WHERE $2=ANY($1)
)
SELECT DISTINCT ON (from_currency_code) from_currency_code,to_currency_code,rate::text,as_of,provider
FROM candidates WHERE rate > 0
ORDER BY from_currency_code,as_of DESC,(provider='identity') DESC,(provider='manual') DESC,provider`,
		fromCurrencies, to, fximport.MoscowDate(asOf),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		From     string
		To       string
		Rate     string
		AsOf     time.Time
		Provider string
	}
	for rows.Next() {
		var row struct {
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
