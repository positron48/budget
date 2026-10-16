package postgres

import (
	"context"
	"encoding/json"
	"math/big"
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
	out, e = readPortfolioQuotes(ctx, tx, tenant, keys, out.ReadAt)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func readPortfolioQuotes(ctx context.Context, tx pgx.Tx, tenant string, keys []string, at time.Time) (nav.PortfolioQuotes, error) {
	out := nav.PortfolioQuotes{ReadAt: at}
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
	metadata, metadataAt, e := readPortfolioQuoteMetadata(ctx, tx, tenant, mappings, at)
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
		if estimate, ok := nav.EstimatedPortfolioQuote(mapping, at); ok {
			estimate.Instrument, estimate.MetadataAt = metadata[key], metadataAt[key]
			out.Quotes = append(out.Quotes, estimate)
			continue
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
		var bond *md.SavedBondFacts
		if terms.Kind == "bond" && price.State == "fresh" && price.Fact != nil {
			bond, e = readPortfolioBondFacts(ctx, tx, selection.Scope, terms.InstrumentID, price.Fact.EffectiveAt, out.ReadAt)
			if e != nil {
				return out, e
			}
		}
		quote, e := nav.NormalizePortfolioQuoteWithBondFacts(mapping, price, out.ReadAt, bond)
		if e != nil {
			return out, e
		}
		quote.Instrument, quote.MetadataAt = metadata[key], metadataAt[key]
		if terms.Kind == "bond" && quote.CleanPrice != nil && bond != nil {
			day := quote.At.UTC().Truncate(24 * time.Hour)
			quote.Accrued = trackerDatedAccrued([]md.SavedBondFacts{*bond}, terms.InstrumentID, quote.Currency, day, at)
			if quote.Accrued != nil {
				quote.AccruedAt = day
			}
			if nominal, ok := nav.PortfolioBondNominal(bond, terms.InstrumentID, quote.Currency, day, at); ok {
				quote.Nominal, _ = new(big.Rat).SetString(nominal.Amount)
				quote.NominalCurrency = nominal.Currency
			}
		}
		out.Quotes = append(out.Quotes, quote)
	}
	return out, nil
}

const portfolioQuoteMetadataSQL = `SELECT r.key,i.data,i.received_at FROM jsonb_to_recordset($2::jsonb) AS r(key text,provider text,scope text,id text)
JOIN market_data_sources s ON s.tenant_id=$1 AND s.provider=r.provider AND s.access_scope=r.scope AND s.enabled
JOIN market_provider_instruments i ON i.tenant_id=s.tenant_id AND i.provider=s.provider AND i.access_scope=s.access_scope AND i.instrument_id=r.id::uuid AND i.received_at<=$3`

// One bounded metadata read for the current reviewed listings. Source enablement
// and tenant predicates match price evidence; ticker guesses never link listings.
func readPortfolioQuoteMetadata(ctx context.Context, tx pgx.Tx, tenant string, mappings map[string]md.Mapping, at time.Time) (map[string]*md.Instrument, map[string]time.Time, error) {
	type ref struct {
		Key      string `json:"key"`
		Provider string `json:"provider"`
		Scope    string `json:"scope"`
		ID       string `json:"id"`
	}
	refs := []ref{}
	for key, m := range mappings {
		for _, p := range m.Periods {
			if !at.Before(p.From) && (p.To.IsZero() || at.Before(p.To)) {
				refs = append(refs, ref{key, p.Provider, p.AccessScope, p.InstrumentID})
				break
			}
		}
	}
	result := map[string]*md.Instrument{}
	times := map[string]time.Time{}
	if len(refs) == 0 {
		return result, times, nil
	}
	raw, e := json.Marshal(refs)
	if e != nil {
		return nil, nil, e
	}
	rows, e := tx.Query(ctx, portfolioQuoteMetadataSQL, tenant, raw, at)
	if e != nil {
		return nil, nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var data []byte
		var received time.Time
		var i md.Instrument
		if e = rows.Scan(&key, &data, &received); e != nil {
			return nil, nil, e
		}
		if json.Unmarshal(data, &i) != nil || md.ValidateInstrument(i) != nil {
			return nil, nil, d.ErrInvestmentPrecondition
		}
		// The joined ID is the reviewed source identity, not an imported ticker.
		for _, r := range refs {
			if r.Key == key && r.ID != i.ID {
				return nil, nil, d.ErrInvestmentPrecondition
			}
		}
		result[key] = &i
		times[key] = received
	}
	return result, times, rows.Err()
}
