package postgres

import (
	"context"
	"time"

	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func PortfolioQuotesSHA256(quotes []PortfolioQuoteSeed) string { return investmentHash(quotes) }

// ImportPortfolioQuotes adds explicitly reviewed quote-only mappings and caches.
// The dry run exercises the same transaction and always rolls it back. Existing
// mappings require their exact expected version; journal/FX/household are untouched.
func (r *MarketDataRepo) ImportPortfolioQuotes(ctx context.Context, tenant, actor string, quotes []PortfolioQuoteSeed, expected string, apply bool) (nav.PortfolioQuotes, error) {
	var out nav.PortfolioQuotes
	if len(quotes) == 0 || len(quotes) > 500 || expected != PortfolioQuotesSHA256(quotes) {
		return out, d.ErrInvestmentInvalid
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, e = tx.Exec(ctx, `SET LOCAL lock_timeout='10s'; SET LOCAL statement_timeout='45s'`); e != nil {
		return out, e
	}
	if e = groupAccess(ctx, tx, tenant, actor, true); e != nil {
		return out, e
	}
	// Match SaveMapping's ordering before taking catalog row locks. Provider
	// workers take the entitlement lock but never the mapping advisory lock.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market-mapping:"+tenant); e != nil {
		return out, e
	}
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market:"+tenant+":tinvest:personal"); e != nil {
		return out, e
	}
	keys, seen := []string{}, map[string]bool{}
	for _, q := range quotes {
		if seen[q.Mapping.InstrumentKey] || md.ValidateInstrument(q.Instrument) != nil || !q.Instrument.Detailed || len(q.Mapping.Periods) != 1 || md.ValidateMappingChange(tenant, q.Mapping) != nil {
			return out, d.ErrInvestmentInvalid
		}
		p := q.Mapping.Periods[0]
		if p.Provider != scope.Provider || p.AccessScope != scope.AccessScope || p.InstrumentID != q.Instrument.ID || p.Kind != q.Instrument.Kind || q.Observation.InstrumentID != q.Instrument.ID || q.Observation.Kind != "last_price" || q.Observation.SourceKind != "exchange" || q.Observation.Adjustment != "unadjusted" {
			return out, d.ErrInvestmentPrecondition
		}
		// Validate the supplied evidence independently; a fresh server cache
		// must not conceal an expired or invalid observation in the reviewed file.
		now := time.Now().UTC()
		normalized, err := md.Normalize(q.Observation)
		if err != nil {
			return out, err
		}
		selected, err := md.SelectValuationPrice(md.PriceSelectionRequest{Scope: scope, InstrumentID: p.InstrumentID, Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", At: now, MaxQuoteAge: nav.PortfolioQuoteMaxAge}, []md.ScopedObservation{{Scope: scope, SavedObservation: md.SavedObservation{Observation: normalized, ID: "reviewed-import", Revision: 1, ReceivedAt: now}}})
		if err != nil {
			return out, err
		}
		quote, err := nav.NormalizePortfolioQuote(md.Mapping{InstrumentKey: q.Mapping.InstrumentKey, Version: 1, Periods: q.Mapping.Periods}, selected, now)
		if err != nil || quote.State != "fresh" || quote.CleanPrice == nil {
			return out, d.ErrInvestmentPrecondition
		}
		seen[q.Mapping.InstrumentKey] = true
		keys = append(keys, q.Mapping.InstrumentKey)
		if _, e = saveMarketInstrument(ctx, tx, scope, q.Instrument); e != nil {
			return out, e
		}
		if _, e = saveMarketMapping(ctx, tx, tenant, actor, q.Mapping); e != nil {
			return out, e
		}
		if _, e = saveMarketObservations(ctx, tx, scope, []md.Observation{q.Observation}); e != nil {
			return out, e
		}
	}
	out, e = readPortfolioQuotes(ctx, tx, tenant, keys, time.Now().UTC().Truncate(time.Microsecond))
	if e != nil {
		return out, e
	}
	for _, q := range out.Quotes {
		if q.State != "fresh" || q.CleanPrice == nil {
			return out, d.ErrInvestmentPrecondition
		}
	}
	if len(out.Quotes) != len(quotes) {
		return out, d.ErrInvestmentPrecondition
	}
	if apply {
		e = tx.Commit(ctx)
	}
	return out, e
}
