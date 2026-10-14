package postgres

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type HistoricalPriceListing struct {
	Provider, AccessScope string
	Instrument            md.Instrument
	Observations          []md.Observation
	ResponseHashes        []string
}

// HistoricalPriceBatch replaces only historical schedules and provider caches.
// Mappings must omit live Periods; their existing values are preserved in the TX.
type HistoricalPriceBatch struct {
	Version  int
	Listings []HistoricalPriceListing
	Mappings []md.MappingChange
}

type HistoricalPriceImport struct {
	InputSHA256                      string
	Listings, Mappings, Observations int
	UnusableObservations             int
	Applied, Reused                  bool
}

func HistoricalPricesSHA256(batch HistoricalPriceBatch) string { return investmentHash(batch) }

// ImportHistoricalPrices rehearses and applies the same atomic transaction.
// A completed retry is a no-op: it must not replace a newer provider correction
// with an old input file. Journal, payouts, live schedules, FX and assets are untouched.
func (r *MarketDataRepo) ImportHistoricalPrices(ctx context.Context, tenant, actor string, batch HistoricalPriceBatch, expected string, apply bool) (HistoricalPriceImport, error) {
	out := HistoricalPriceImport{InputSHA256: expected, Listings: len(batch.Listings), Mappings: len(batch.Mappings)}
	if batch.Version != 1 || expected != HistoricalPricesSHA256(batch) || len(batch.Listings) == 0 || len(batch.Listings) > 500 || len(batch.Mappings) == 0 || len(batch.Mappings) > 500 {
		return out, d.ErrInvestmentInvalid
	}
	listingKeys := map[string]HistoricalPriceListing{}
	scopes := map[string]md.Scope{}
	for _, l := range batch.Listings {
		scope := md.Scope{TenantID: tenant, Provider: l.Provider, AccessScope: l.AccessScope}
		key := l.Provider + "/" + l.AccessScope + "/" + l.Instrument.ID
		if _, exists := listingKeys[key]; exists || md.ValidateScope(scope) != nil || l.Provider != "moex" && l.Provider != "twelvedata" || md.ValidateInstrument(l.Instrument) != nil || !l.Instrument.Detailed || len(l.Observations) == 0 || len(l.Observations) > 4000 || len(l.ResponseHashes) == 0 || len(l.ResponseHashes) > 60 {
			return out, d.ErrInvestmentInvalid
		}
		for _, hash := range l.ResponseHashes {
			raw, err := hex.DecodeString(hash)
			if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != hash {
				return out, d.ErrInvestmentInvalid
			}
		}
		for i, o := range l.Observations {
			if o.QualityIssue != "" {
				out.UnusableObservations++
			}
			normal, err := md.Normalize(o)
			if err != nil {
				return out, err
			}
			price, ok := new(big.Rat).SetString(normal.Price)
			if !ok || price.Sign() <= 0 || o.InstrumentID != l.Instrument.ID || o.Kind != "daily_candle" || o.SourceKind != "exchange" || o.Adjustment != "unadjusted" || !o.Complete || o.Currency != l.Instrument.Currency || !o.At.Equal(o.At.UTC().Truncate(24*time.Hour)) || !o.At.Before(time.Now().UTC().Truncate(24*time.Hour)) || i > 0 && !o.At.After(l.Observations[i-1].At) {
				return out, d.ErrInvestmentInvalid
			}
			switch l.Instrument.Kind {
			case "bond":
				if o.PriceUnit != "percent_of_nominal" || o.Nominal == "" {
					return out, d.ErrInvestmentInvalid
				}
			case "share", "etf":
				if o.PriceUnit != "currency_per_unit" {
					return out, d.ErrInvestmentInvalid
				}
			default:
				return out, d.ErrInvestmentInvalid
			}
		}
		listingKeys[key] = l
		scopes[l.Provider+"/"+l.AccessScope] = scope
		out.Observations += len(l.Observations)
	}
	if out.Observations > 1000000 {
		return out, d.ErrInvestmentLimit
	}
	seen := map[string]bool{}
	for _, c := range batch.Mappings {
		if seen[c.InstrumentKey] || c.Periods != nil || c.HistoricalPeriods == nil || len(*c.HistoricalPeriods) == 0 || md.ValidateMappingChange(tenant, c) != nil {
			return out, d.ErrInvestmentInvalid
		}
		seen[c.InstrumentKey] = true
		for _, p := range *c.HistoricalPeriods {
			l, ok := listingKeys[p.Provider+"/"+p.AccessScope+"/"+p.InstrumentID]
			if !ok || p.Currency != l.Instrument.Currency || p.Kind != l.Instrument.Kind || p.Kind == "bond" && p.Unit != "percent_of_nominal" || p.Kind != "bond" && p.Unit != "currency_per_unit" {
				return out, d.ErrInvestmentInvalid
			}
		}
	}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='10s'; SET LOCAL statement_timeout='90s'`); err != nil {
		return out, err
	}
	if err = groupAccess(ctx, tx, tenant, actor, true); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market-mapping:"+tenant); err != nil {
		return out, err
	}
	// Check every request before writing any observation. Existing revision data
	// preserves the live schedule used in its original idempotency hash.
	reused := 0
	for _, c := range batch.Mappings {
		var hash string
		var raw []byte
		err = tx.QueryRow(ctx, `SELECT request_hash,data FROM investment_market_mapping_revisions WHERE tenant_id=$1 AND request_key=$2`, tenant, c.RequestKey).Scan(&hash, &raw)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return out, err
		}
		var previous md.Mapping
		if json.Unmarshal(raw, &previous) != nil {
			return out, d.ErrInvestmentPrecondition
		}
		c.Periods = previous.Periods
		c.Reason = historicalImportReason(expected, c.Reason)
		if hash != investmentHash(struct {
			Actor  string
			Change md.MappingChange
		}{actor, c}) {
			return out, md.ErrConflict
		}
		reused++
	}
	if reused > 0 {
		if reused != len(batch.Mappings) {
			return out, md.ErrConflict
		}
		out.Reused = true
		return out, nil
	}
	scopeKeys := []string{}
	for key := range scopes {
		scopeKeys = append(scopeKeys, key)
	}
	sort.Strings(scopeKeys)
	for _, key := range scopeKeys {
		s := scopes[key]
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market:"+tenant+":"+s.Provider+":"+s.AccessScope); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO market_data_sources(tenant_id,provider,access_scope,title,enabled) VALUES($1,$2,$3,$4,true) ON CONFLICT DO NOTHING`, tenant, s.Provider, s.AccessScope, s.Provider+" native historical prices"); err != nil {
			return out, err
		}
		var enabled bool
		if err = tx.QueryRow(ctx, `SELECT enabled FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 FOR SHARE`, tenant, s.Provider, s.AccessScope).Scan(&enabled); err != nil {
			return out, err
		}
		if !enabled {
			return out, d.ErrInvestmentPrecondition
		}
	}
	for _, l := range batch.Listings {
		s := scopes[l.Provider+"/"+l.AccessScope]
		if _, err = saveMarketInstrument(ctx, tx, s, l.Instrument); err != nil {
			return out, err
		}
		for start := 0; start < len(l.Observations); start += 1000 {
			end := min(start+1000, len(l.Observations))
			if err = saveHistoricalObservations(ctx, tx, s, l.Observations[start:end]); err != nil {
				return out, err
			}
		}
	}
	for _, c := range batch.Mappings {
		var raw []byte
		err = tx.QueryRow(ctx, `SELECT data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=$2 FOR UPDATE`, tenant, c.InstrumentKey).Scan(&raw)
		if err == nil {
			var old md.Mapping
			if json.Unmarshal(raw, &old) != nil {
				return out, d.ErrInvestmentPrecondition
			}
			c.Periods = old.Periods
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		c.Reason = historicalImportReason(expected, c.Reason)
		if _, err = saveMarketMapping(ctx, tx, tenant, actor, c); err != nil {
			return out, err
		}
	}
	if apply {
		if err = tx.Commit(ctx); err != nil {
			return out, err
		}
		out.Applied = true
	}
	return out, nil
}

func historicalImportReason(hash, reason string) string {
	return "Native historical price batch " + hash + ": " + reason
}
