package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// InvestmentQuotes reads only saved, entitled market evidence. It does not
// depend on broker holdings, trigger provider requests or register NAV series.
func (r *MarketDataRepo) InvestmentQuotes(ctx context.Context, tenant, actor string, input []string) (nav.PortfolioQuotes, error) {
	out := nav.PortfolioQuotes{ReadAt: time.Now().UTC().Truncate(time.Microsecond)}
	keys, e := nav.NormalizeQuoteKeys(input)
	if e != nil {
		return out, e
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = groupAccess(ctx, tx, tenant, actor, false); e != nil {
		return out, e
	}
	rows, e := tx.Query(ctx, `SELECT instrument_key,data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=ANY($2::text[])`, tenant, keys)
	if e != nil {
		return out, e
	}
	mappings := map[string]md.Mapping{}
	for rows.Next() {
		var key string
		var raw []byte
		var m md.Mapping
		if e = rows.Scan(&key, &raw); e != nil {
			rows.Close()
			return out, e
		}
		if e = json.Unmarshal(raw, &m); e != nil {
			rows.Close()
			return out, e
		}
		mappings[key] = m
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for _, key := range keys {
		mapping, ok := mappings[key]
		if !ok {
			out.Quotes = append(out.Quotes, nav.PortfolioQuote{InstrumentKey: key, State: "unavailable", Issues: []string{"mapping_missing"}})
			continue
		}
		if mapping.InstrumentKey != key || mapping.Version < 1 {
			return out, d.ErrInvestmentPrecondition
		}
		terms, ok := mapping.TermsAt(out.ReadAt)
		if !ok {
			out.Quotes = append(out.Quotes, nav.PortfolioQuote{InstrumentKey: key, State: "unavailable", MappingVersion: mapping.Version, Issues: []string{"terms_missing"}})
			continue
		}
		var period md.MappingPeriod
		for _, p := range mapping.Periods {
			if !out.ReadAt.Before(p.From) && (p.To.IsZero() || out.ReadAt.Before(p.To)) {
				period = p
				break
			}
		}
		selection := md.PriceSelectionRequest{Scope: md.Scope{TenantID: tenant, Provider: period.Provider, AccessScope: period.AccessScope}, InstrumentID: terms.InstrumentID, Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", At: out.ReadAt, MaxQuoteAge: nav.PortfolioQuoteMaxAge}
		from := out.ReadAt.Add(-30 * 24 * time.Hour)
		if terms.From.After(from) {
			from = terms.From
		}
		observations, e := readPriceEvidenceMode(ctx, tx, selection, from, true)
		if e != nil {
			return out, e
		}
		price, e := md.SelectValuationPrice(selection, observations)
		if e != nil {
			return out, e
		}
		quote, e := nav.NormalizePortfolioQuote(mapping, price, out.ReadAt)
		if e != nil {
			return out, e
		}
		out.Quotes = append(out.Quotes, quote)
	}
	return out, tx.Commit(ctx)
}
