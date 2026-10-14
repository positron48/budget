package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	av "github.com/positron48/budget/internal/adapter/alphavantage"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type PortfolioMarketInput struct {
	AlphaPolicy    md.FXPolicy
	AlphaAudits    []av.Audit
	OfficialPolicy md.FXPolicy
	OfficialPoints []md.FXPoint
	Quotes         []PortfolioQuoteSeed
}
type PortfolioQuoteSeed struct {
	Instrument  md.Instrument
	Observation md.Observation
	Mapping     md.MappingChange
}

func PortfolioMarketSHA256(input PortfolioMarketInput) string { return investmentHash(input) }

func validatePortfolioMarket(tenant string, input PortfolioMarketInput) error {
	if md.ValidateFXPolicy(input.AlphaPolicy) != nil || input.AlphaPolicy.Alpha == nil || len(input.AlphaAudits) != 2 || len(input.Quotes) > 500 {
		return d.ErrInvestmentInvalid
	}
	pairs := map[string]bool{}
	for _, audit := range input.AlphaAudits {
		if audit.RetrievedAt.After(time.Now().UTC()) || pairs[audit.Pair] || (audit.Pair != "USD/RUB" && audit.Pair != "EUR/RUB") {
			return d.ErrInvestmentInvalid
		}
		pairs[audit.Pair] = true
		if _, e := av.RetrospectivePoints(input.AlphaPolicy, audit); e != nil {
			return e
		}
	}
	if input.AlphaAudits[0].From != input.AlphaAudits[1].From || input.AlphaAudits[0].To != input.AlphaAudits[1].To {
		return d.ErrInvestmentInvalid
	}
	if input.OfficialPolicy.Key != "" && len(input.OfficialPoints) == 0 {
		return d.ErrInvestmentInvalid
	}
	if len(input.OfficialPoints) > 0 {
		if input.OfficialPolicy.Key == input.AlphaPolicy.Key || input.OfficialPolicy.Official == nil || md.ValidateFXPolicy(input.OfficialPolicy) != nil {
			return d.ErrInvestmentInvalid
		}
		from, to := input.OfficialPoints[0].At.Add(3*time.Hour), input.OfficialPoints[0].At.Add(3*time.Hour)
		for _, p := range input.OfficialPoints {
			day := p.At.Add(3 * time.Hour)
			if day.Before(from) {
				from = day
			}
			if day.After(to) {
				to = day
			}
		}
		if md.ValidateOfficialFXPoints(input.OfficialPolicy, input.OfficialPoints, from, to) != nil || to.After(time.Now().UTC().Add(3*time.Hour).Truncate(24*time.Hour)) {
			return d.ErrInvestmentInvalid
		}
	}
	keys := map[string]bool{}
	for quoteIndex, q := range input.Quotes {
		if q.Mapping.ExpectedVersion != 0 || q.Observation.At.After(time.Now().UTC()) || keys[q.Mapping.InstrumentKey] || md.ValidateInstrument(q.Instrument) != nil || md.ValidateMappingChange(tenant, q.Mapping) != nil || len(q.Mapping.Periods) != 1 {
			return d.ErrInvestmentInvalid
		}
		keys[q.Mapping.InstrumentKey] = true
		p := q.Mapping.Periods[0]
		if p.Provider != "tinvest" || p.AccessScope != "personal" || p.InstrumentID != q.Instrument.ID || p.Kind != q.Instrument.Kind || q.Observation.InstrumentID != q.Instrument.ID || q.Observation.Kind != "last_price" || q.Observation.SourceKind != "exchange" || q.Observation.Adjustment != "unadjusted" {
			return d.ErrInvestmentPrecondition
		}
		observation, e := md.Normalize(q.Observation)
		if e != nil {
			return fmt.Errorf("normalize quote %d: %w", quoteIndex, e)
		}
		now := time.Now().UTC()
		scope := md.Scope{TenantID: tenant, Provider: p.Provider, AccessScope: p.AccessScope}
		selected, e := md.SelectValuationPrice(md.PriceSelectionRequest{Scope: scope, InstrumentID: p.InstrumentID, Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", At: now, MaxQuoteAge: nav.PortfolioQuoteMaxAge}, []md.ScopedObservation{{Scope: scope, SavedObservation: md.SavedObservation{Observation: observation, ID: "imported", Revision: 1, ReceivedAt: now}}})
		if e != nil {
			return fmt.Errorf("select quote %d: %w", quoteIndex, e)
		}
		normalized, e := nav.NormalizePortfolioQuote(md.Mapping{InstrumentKey: q.Mapping.InstrumentKey, Version: 1, Periods: q.Mapping.Periods}, selected, now)
		if e != nil || normalized.CleanPrice == nil || normalized.State != "fresh" {
			return d.ErrInvestmentPrecondition
		}
	}
	return nil
}

// ConfigurePortfolioMarket belongs to isolated candidate construction. Policies
// and reviewed mappings are canonical archive facts; provider caches are separate.
func (r *MarketDataRepo) ConfigurePortfolioMarket(ctx context.Context, tenant, actor string, input PortfolioMarketInput) error {
	if e := validatePortfolioMarket(tenant, input); e != nil {
		return fmt.Errorf("market plan validation: %w", e)
	}
	for _, p := range []md.FXPolicy{input.AlphaPolicy, input.OfficialPolicy} {
		if p.Key == "" {
			continue
		}
		if _, e := r.RegisterFXPolicy(ctx, tenant, actor, p, 0); e != nil {
			return fmt.Errorf("register FX policy: %w", e)
		}
	}
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if len(input.Quotes) > 0 {
		if e := r.ConfigureSource(ctx, scope, "Imported exchange evidence", true); e != nil {
			return e
		}
	}
	for _, q := range input.Quotes {
		tx, e := r.pool.DB.Begin(ctx)
		if e != nil {
			return e
		}
		_, e = saveMarketInstrument(ctx, tx, scope, q.Instrument)
		if e != nil {
			_ = tx.Rollback(ctx)
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		if _, e = r.SaveMapping(ctx, tenant, actor, q.Mapping); e != nil {
			return fmt.Errorf("save reviewed mapping: %w", e)
		}
	}
	if e := r.PublishPortfolioMarket(ctx, tenant, input); e != nil {
		return fmt.Errorf("publish market cache: %w", e)
	}
	return nil
}

// Lock market/FX namespaces BEFORE classified archive tables to avoid lock-order
// inversion with normal provider workers during atomic replacement.
func lockPortfolioMarket(ctx context.Context, tx pgx.Tx, tenant string, input PortfolioMarketInput) error {
	locks := map[string]bool{}
	for _, p := range []md.FXPolicy{input.AlphaPolicy, input.OfficialPolicy} {
		if p.Key != "" {
			if len(p.Series) > 0 {
				locks["market:"+tenant+":"+p.Source+":"+p.AccessScope] = true
			}
			locks["investment-fx:"+tenant+":"+p.Key] = true
		}
	}
	if len(input.Quotes) > 0 {
		locks["market:"+tenant+":tinvest:personal"] = true
	}
	// Existing policy registration locks market before investment-fx.
	keys := []string{}
	for key := range locks {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		im, jm := keys[i][:7] == "market:", keys[j][:7] == "market:"
		if im != jm {
			return im
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); e != nil {
			return e
		}
	}
	return nil
}

func (r *MarketDataRepo) PublishPortfolioMarket(ctx context.Context, tenant string, input PortfolioMarketInput) error {
	if e := validatePortfolioMarket(tenant, input); e != nil {
		return e
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = lockPortfolioMarket(ctx, tx, tenant, input); e != nil {
		return e
	}
	if e = publishPortfolioMarketTx(ctx, tx, tenant, input); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func publishPortfolioMarketTx(ctx context.Context, tx pgx.Tx, tenant string, input PortfolioMarketInput) error {
	policies := []md.FXPolicy{input.AlphaPolicy}
	if len(input.OfficialPoints) > 0 {
		policies = append(policies, input.OfficialPolicy)
	}
	for _, p := range policies {
		var version int64
		var raw []byte
		e := tx.QueryRow(ctx, `SELECT p.version,r.data FROM investment_fx_policies p JOIN investment_fx_policy_revisions r USING(tenant_id,policy_key,version) WHERE p.tenant_id=$1 AND p.policy_key=$2 AND p.enabled FOR SHARE OF p`, tenant, p.Key).Scan(&version, &raw)
		if e != nil {
			return e
		}
		var stored md.FXPolicy
		if json.Unmarshal(raw, &stored) != nil || version != 1 || !reflect.DeepEqual(stored, p) {
			return d.ErrInvestmentPrecondition
		}
		points := input.OfficialPoints
		if p.Alpha != nil {
			points = nil
			for _, audit := range input.AlphaAudits {
				normalized, e := av.RetrospectivePoints(p, audit)
				if e != nil {
					return e
				}
				points = append(points, normalized...)
			}
		}
		for len(points) > 0 {
			n := min(1000, len(points))
			if _, e = saveFXPoints(ctx, tx, tenant, p.Key, version, points[:n]); e != nil {
				return e
			}
			points = points[n:]
		}
	}
	if len(input.Quotes) > 0 {
		scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
		if _, e := tx.Exec(ctx, `INSERT INTO market_data_sources(tenant_id,provider,access_scope,title,enabled) VALUES($1,'tinvest','personal','Imported exchange evidence',true) ON CONFLICT(tenant_id,provider,access_scope) DO UPDATE SET enabled=true`, tenant); e != nil {
			return e
		}
		for _, q := range input.Quotes {
			var raw []byte
			if e := tx.QueryRow(ctx, `SELECT data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=$2`, tenant, q.Mapping.InstrumentKey).Scan(&raw); e != nil {
				return e
			}
			var stored md.Mapping
			if json.Unmarshal(raw, &stored) != nil || stored.Version != 1 || !reflect.DeepEqual(stored.Periods, q.Mapping.Periods) {
				return d.ErrInvestmentPrecondition
			}
			if _, e := saveMarketInstrument(ctx, tx, scope, q.Instrument); e != nil {
				return e
			}
			if _, e := saveMarketObservations(ctx, tx, scope, []md.Observation{q.Observation}); e != nil {
				return e
			}
		}
	}
	return nil
}
