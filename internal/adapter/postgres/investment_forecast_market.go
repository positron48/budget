package postgres

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"sort"
	"time"
)

const forecastMarketSQL = `WITH periods AS (
 SELECT m.instrument_key,p->>'Provider' provider,p->>'AccessScope' access_scope,(p->>'InstrumentID')::uuid instrument_id
 FROM investment_market_mappings m CROSS JOIN LATERAL jsonb_array_elements(
 CASE WHEN jsonb_array_length(COALESCE(NULLIF(m.data->'Periods','null'::jsonb),'[]'::jsonb))>0 THEN m.data->'Periods' ELSE COALESCE(NULLIF(m.data->'HistoricalPeriods','null'::jsonb),'[]'::jsonb) END) p
 WHERE m.tenant_id=$1 AND m.instrument_key=ANY($2::text[])
 AND (p->>'From')::timestamptz<=$3 AND ((p->>'To')::timestamptz>$3 OR p->>'To'='0001-01-01T00:00:00Z')
), listings AS (SELECT DISTINCT instrument_key,provider,access_scope,instrument_id FROM periods)
SELECT l.instrument_key,l.provider,l.access_scope,'bond',b.data FROM listings l
JOIN market_data_sources s ON s.tenant_id=$1 AND s.provider=l.provider AND s.access_scope=l.access_scope AND s.enabled
JOIN LATERAL (SELECT jsonb_build_object('Coupons',data->'Coupons','Instrument',data->'Instrument','Terms',data->'Terms') data FROM market_bond_snapshots b
 WHERE b.tenant_id=$1 AND b.provider=l.provider AND b.access_scope=l.access_scope AND b.instrument_id=l.instrument_id AND b.to_at>=$3
 ORDER BY b.recorded_at DESC,b.job_id DESC LIMIT 1) b ON true
UNION ALL
SELECT l.instrument_key,l.provider,l.access_scope,'dividend',b.data FROM listings l
JOIN market_data_sources s ON s.tenant_id=$1 AND s.provider=l.provider AND s.access_scope=l.access_scope AND s.enabled
JOIN LATERAL (SELECT jsonb_build_object('Dividends',data->'Dividends') data FROM market_dividend_snapshots b
 WHERE b.tenant_id=$1 AND b.provider=l.provider AND b.access_scope=l.access_scope AND b.instrument_id=l.instrument_id AND b.to_at>=$3-interval '1 year'
 ORDER BY b.recorded_at DESC,b.job_id DESC LIMIT 1) b ON true
ORDER BY 1,2,3,4 LIMIT 2001`

func readForecastMarket(ctx context.Context, tx pgx.Tx, tenant string, ops []d.InvestmentOperation, at time.Time) ([]inv.PayoutSchedule, string, error) {
	seen := map[string]bool{}
	for _, op := range ops {
		if op.Status == "posted" && op.InstrumentKey != "" && !op.OccurredAt.After(at) {
			seen[op.InstrumentKey] = true
			if op.Distribution != nil {
				seen[op.Distribution.TargetInstrumentKey] = true
			}
		}
	}
	if len(seen) > 10000 {
		return nil, "automatic_schedule_limit", nil
	}
	keys := []string{}
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows, e := tx.Query(ctx, forecastMarketSQL, tenant, keys, at)
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	out := []inv.PayoutSchedule{}
	total := 0
	for rows.Next() {
		var key, provider, scope, kind string
		var raw []byte
		if e = rows.Scan(&key, &provider, &scope, &kind, &raw); e != nil {
			return nil, "", e
		}
		total += len(raw)
		if total > 16<<20 || len(out) >= 2000 {
			return nil, "automatic_schedule_limit", nil
		}
		s := inv.PayoutSchedule{InstrumentKey: key, Reference: provider + "/" + scope + " payout schedule"}
		if kind == "bond" {
			var f md.BondFacts
			if e = json.Unmarshal(raw, &f); e != nil {
				return nil, "", e
			}
			s.Coupons = f.Coupons
			if f.Terms.Floating != nil {
				s.Floating = *f.Terms.Floating
			}
			s.Currency = f.Instrument.Currency
			if f.Terms.Nominal != nil {
				s.Currency = f.Terms.Nominal.Currency
			}
		} else {
			var f md.DividendFacts
			if e = json.Unmarshal(raw, &f); e != nil {
				return nil, "", e
			}
			s.Dividends = f.Dividends
		}
		out = append(out, s)
	}
	return out, "", rows.Err()
}
