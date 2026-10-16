package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// A card needs no legacy NAV registration or owned position. A direct catalog
// listing is a provider series, never a newly confirmed personal mapping.
func (r *MarketDataRepo) TrackerInstrumentPrices(ctx context.Context, tenant, actor string, input nav.TrackerInstrumentRequest) (nav.TrackerInstrumentHistory, error) {
	out := nav.TrackerInstrumentHistory{ReadAt: time.Now().UTC().Truncate(time.Microsecond)}
	q, e := nav.NormalizeTrackerInstrument(input, out.ReadAt)
	if e != nil {
		return out, e
	}
	out.InstrumentKey, out.Currency, out.FXPolicyKey = q.InstrumentKey, q.Currency, q.FXPolicyKey
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var role string
	e = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, tenant, actor).Scan(&role)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && role != "owner" && role != "admin" && role != "member" {
		return out, d.ErrInvestmentForbidden
	}
	if e != nil {
		return out, e
	}
	enabled := map[string]bool{}
	sourceRows, err := tx.Query(ctx, `SELECT provider,access_scope FROM market_data_sources WHERE tenant_id=$1 AND enabled`, tenant)
	if err != nil {
		return out, err
	}
	for sourceRows.Next() {
		var provider, scope string
		if err = sourceRows.Scan(&provider, &scope); err != nil {
			sourceRows.Close()
			return out, err
		}
		enabled[provider+"/"+scope] = true
	}
	if err = sourceRows.Err(); err != nil {
		sourceRows.Close()
		return out, err
	}
	sourceRows.Close()
	var mapping md.Mapping
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=$2`, tenant, q.InstrumentKey).Scan(&raw)
	if e == nil {
		if json.Unmarshal(raw, &mapping) != nil || mapping.InstrumentKey != q.InstrumentKey || mapping.Version < 1 {
			return out, d.ErrInvestmentPrecondition
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if q.Provider != "" {
		mapping = md.Mapping{}
		scope := md.Scope{TenantID: tenant, Provider: q.Provider, AccessScope: q.AccessScope}
		if md.ValidateScope(scope) != nil || md.ValidateRequest(md.JobRequest{Kind: "last_price", InstrumentID: q.InstrumentID}) != nil {
			return out, d.ErrInvestmentInvalid
		}
		i, at, err := readTrackerInstrumentMetadata(ctx, tx, scope, q.InstrumentID)
		if err != nil {
			return out, err
		}
		out.Instrument, out.MetadataAt, out.Scope, out.ListingOnly = i, at, scope, true
		out.MetadataInstrumentID, out.MetadataEnabled = q.InstrumentID, enabled[q.Provider+"/"+q.AccessScope]
		if i != nil {
			unit := "currency_per_unit"
			if i.Kind == "bond" {
				unit = "percent_of_nominal"
			}
			if i.Kind != "share" && i.Kind != "etf" && i.Kind != "bond" {
				return out, d.ErrInvestmentPrecondition
			}
			mapping = md.Mapping{InstrumentKey: q.InstrumentKey, Version: 1, HistoricalPeriods: []md.MappingPeriod{{Provider: q.Provider, AccessScope: q.AccessScope, InstrumentID: i.ID, Kind: i.Kind, Currency: i.Currency, Unit: unit, From: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), Evidence: "Explicit provider listing; not a personal historical identity claim"}}}
		}
	}
	livePeriods := mapping.Periods
	periods := mapping.HistoryPeriods()
	chosen := []md.MappingPeriod{}
	for _, p := range periods {
		id := investmentHash(p)
		out.Listings = append(out.Listings, nav.TrackerInstrumentListing{ID: id, Period: p, Enabled: enabled[p.Provider+"/"+p.AccessScope]})
		if q.ListingID == "" || q.ListingID == id {
			chosen = append(chosen, p)
		}
	}
	if q.ListingID != "" && len(chosen) == 0 {
		return out, d.ErrInvestmentPrecondition
	}
	mapping.HistoricalPeriods = chosen
	mapping.Periods = nil
	if out.Instrument == nil && len(chosen) > 0 {
		p := chosen[len(chosen)-1]
		if !out.ListingOnly {
			for _, current := range livePeriods {
				if !out.ReadAt.Before(current.From) && (current.To.IsZero() || out.ReadAt.Before(current.To)) {
					p = current
					break
				}
			}
		}
		scope := md.Scope{TenantID: tenant, Provider: p.Provider, AccessScope: p.AccessScope}
		i, at, err := readTrackerInstrumentMetadata(ctx, tx, scope, p.InstrumentID)
		if err != nil {
			return out, err
		}
		out.Scope, out.Instrument, out.MetadataAt = scope, i, at
		out.MetadataInstrumentID, out.MetadataEnabled = p.InstrumentID, enabled[p.Provider+"/"+p.AccessScope]
		if out.Instrument == nil && p.NativeInstrument != nil && enabled[p.Provider+"/"+p.AccessScope] {
			out.Instrument = p.NativeInstrument
		}
	}
	// Current quotes stay independent of selected historical listing periods.
	quotes, err := readPortfolioQuotes(ctx, tx, tenant, []string{q.InstrumentKey}, out.ReadAt)
	if err != nil {
		return out, err
	}
	if len(quotes.Quotes) > 0 {
		out.Quote = &quotes.Quotes[0]
	}
	if out.Instrument != nil && out.Instrument.Kind == "bond" {
		out.Bond, e = readTrackerBondInfo(ctx, tx, out.Scope, out.Instrument.ID, out.ReadAt)
		if e != nil {
			return out, e
		}
		if out.Bond != nil && out.Bond.AsOf.After(out.ReadAt) {
			out.Bond = nil
		}
	}
	out.Bond, out.BondLimited = trackerCardBondExcerpt(out.Bond, out.ReadAt)
	rootTo, _ := nav.DayCutoff(q.To)
	var policy md.SavedFXPolicy
	var fx []md.SavedFXPoint
	out.FXMode = "unavailable"
	if q.FXPolicyKey != "" {
		policy, fx, e = readFXSnapshot(ctx, tx, tenant, q.FXPolicyKey, rootTo, time.Time{})
		if e != nil && !errors.Is(e, md.ErrNotFound) {
			return out, e
		}
		if e == nil {
			out.FXPolicyVersion, out.FXMode = policy.Version, policy.Mode
		}
	}
	fixings := []calc.FXFixing{}
	for _, p := range fx {
		if !p.AvailableAt.After(rootTo) {
			fixings = append(fixings, p.Fixing())
		}
	}
	requested := []trackerHistorySeries{}
	for _, p := range chosen {
		requested = append(requested, trackerHistorySeries{p.Provider, p.AccessScope, p.InstrumentID})
	}
	list, _ := json.Marshal(requested)
	var basis json.RawMessage
	if e = tx.QueryRow(ctx, trackerInstrumentBasisSQL, tenant, list, rootTo).Scan(&basis); e != nil {
		return out, e
	}
	out.BasisKey = investmentHash(struct {
		Mapping    md.Mapping
		Policy     md.SavedFXPolicy
		FX         []md.SavedFXPoint
		Basis      json.RawMessage
		Instrument *md.Instrument
	}{mapping, policy, fx, basis, out.Instrument})
	first, _ := time.Parse(time.DateOnly, q.From)
	last, _ := time.Parse(time.DateOnly, q.To)
	end := first.AddDate(0, 0, nav.TrackerPageDays(q.PageDays)-1)
	if end.After(last) {
		end = last
	} else if end.Before(last) {
		out.NextDate = end.AddDate(0, 0, 1).Format(time.DateOnly)
	}
	firstAt, _ := nav.DayCutoff(q.From)
	endAt, _ := nav.DayCutoff(end.Format(time.DateOnly))
	entries, e := readTrackerHistorySeries(ctx, tx, tenant, map[string]trackerHistoryMarket{q.InstrumentKey: {mapping: mapping}}, firstAt, endAt, out.ReadAt)
	if e != nil {
		return out, e
	}
	entry := entries[q.InstrumentKey]
	for day := first; !day.After(end); day = day.AddDate(0, 0, 1) {
		point := nav.TrackerInstrumentPoint{Date: day.Format(time.DateOnly), State: "unavailable", Issues: []string{}}
		cutoff, _ := nav.DayCutoff(point.Date)
		var p *md.MappingPeriod
		for i := range chosen {
			v := &chosen[i]
			if !cutoff.Before(v.From) && (v.To.IsZero() || cutoff.Before(v.To)) {
				p = v
				break
			}
		}
		if p == nil {
			point.Issues = append(point.Issues, "mapping_period_missing")
			out.Points = append(out.Points, point)
			continue
		}
		point.NativeCurrency, point.Unit, point.Source = p.Currency, p.Unit, p.Provider+"/"+p.AccessScope
		selected, err := entry.selectPrice(historyPriceSelection(tenant, *p, cutoff), historyPeriodKey(*p))
		if err != nil {
			return out, err
		}
		if selected.Fact == nil || selected.Fact.EffectiveAt.Before(p.From) {
			point.Issues = append(point.Issues, selected.Issues...)
			point.Issues = append(point.Issues, "price_missing")
			out.Points = append(out.Points, point)
			continue
		}
		raw, ok := new(big.Rat).SetString(selected.Fact.Price)
		if !ok || raw.Sign() <= 0 {
			return out, d.ErrInvestmentPrecondition
		}
		if selected.Fact.Currency != "" {
			point.NativeCurrency = selected.Fact.Currency
		}
		if selected.Fact.Unit != "" && selected.Fact.Unit != "provider_unspecified" {
			point.Unit = selected.Fact.Unit
		}
		point.Raw, point.ObservationID, point.Revision = raw, selected.ObservationID, selected.ObservationRevision
		observedDay := selected.Fact.EffectiveAt.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
		point.CandleDate = observedDay.Format(time.DateOnly)
		quoteEntry := entry
		if out.ListingOnly {
			direct := *p
			if selected.Fact.Currency != "" {
				direct.Currency = selected.Fact.Currency
			}
			if direct.Kind == "bond" && selected.Fact.Currency == "" {
				for j := range entry.bonds[historyPeriodKey(*p)] {
					f := &entry.bonds[historyPeriodKey(*p)][j]
					if f.Terms.Nominal != nil {
						if _, ok := nav.PortfolioBondNominal(f, p.InstrumentID, f.Terms.Nominal.Currency, observedDay, out.ReadAt); ok {
							direct.Currency = f.Terms.Nominal.Currency
						}
						break
					}
				}
			}
			quoteEntry.mapping = entry.mapping
			quoteEntry.mapping.HistoricalPeriods = []md.MappingPeriod{direct}
		}
		quote, err := trackerHistoricalQuote(tenant, q.InstrumentKey, quoteEntry, cutoff, out.ReadAt)
		if err != nil {
			return out, err
		}
		point.NativeClean = quote.CleanPrice
		if point.NativeClean != nil {
			point.NativeCurrency = quote.Currency
		}
		point.Issues = append(point.Issues, quote.Issues...)
		point.State = "ready"
		for _, o := range entry.observations[historyPeriodKey(*p)] {
			if o.ID == point.ObservationID && o.Revision == point.Revision {
				point.Nominal, point.NominalCurrency = o.Nominal, o.NominalCurrency
				break
			}
		}
		if p.Kind == "bond" {
			point.Accrued = trackerDatedAccrued(entry.bonds[historyPeriodKey(*p)], p.InstrumentID, point.NativeCurrency, observedDay, out.ReadAt)
			if point.Accrued == nil {
				point.Issues = append(point.Issues, "accrued_interest_missing")
			}
			if point.Accrued != nil && point.NativeClean != nil {
				point.NativeDirty = new(big.Rat).Add(point.NativeClean, point.Accrued)
			}
		} else {
			point.NativeDirty = point.NativeClean
		}
		if point.NativeClean != nil {
			if point.NativeCurrency == q.Currency {
				point.Clean, point.Dirty = point.NativeClean, point.NativeDirty
			} else if policy.Version > 0 {
				resolver, err := calc.NewFXResolver(fixings, md.FXPolicyRef(policy.Key, policy.Version), cutoff)
				if err != nil {
					return out, err
				}
				rate, found := resolver.Resolve(point.NativeCurrency, q.Currency)
				if found {
					point.Clean = new(big.Rat).Mul(point.NativeClean, rate.Rate)
					if point.NativeDirty != nil {
						point.Dirty = new(big.Rat).Mul(point.NativeDirty, rate.Rate)
					}
				}
			}
		}
		if point.Clean == nil {
			point.State = "incomplete"
			point.Issues = append(point.Issues, "price_or_fx_missing")
		}
		out.Points = append(out.Points, point)
	}
	return out, tx.Commit(ctx)
}

func readTrackerInstrumentMetadata(ctx context.Context, tx pgx.Tx, s md.Scope, id string) (*md.Instrument, time.Time, error) {
	var raw []byte
	var at time.Time
	e := tx.QueryRow(ctx, `SELECT i.data,i.received_at FROM market_provider_instruments i JOIN market_data_sources s USING(tenant_id,provider,access_scope) WHERE i.tenant_id=$1 AND i.provider=$2 AND i.access_scope=$3 AND i.instrument_id=$4 AND s.enabled`, s.TenantID, s.Provider, s.AccessScope, id).Scan(&raw, &at)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, at, nil
	}
	if e != nil {
		return nil, at, e
	}
	var i md.Instrument
	if json.Unmarshal(raw, &i) != nil || i.ID != id || md.ValidateInstrument(i) != nil {
		return nil, at, d.ErrInvestmentPrecondition
	}
	return &i, at, nil
}

func trackerDatedAccrued(facts []md.SavedBondFacts, id, currency string, day, readAt time.Time) *big.Rat {
	var latest *md.SavedBondFacts
	for i := range facts {
		f := &facts[i]
		if f.Instrument.ID == id && !f.RecordedAt.After(readAt) && !f.AsOf.After(readAt) && !day.After(f.AsOf.UTC().Truncate(24*time.Hour)) && !day.Before(f.From) && !day.After(f.To) && (latest == nil || f.RecordedAt.After(latest.RecordedAt)) {
			latest = f
		}
	}
	if latest == nil {
		return nil
	}
	var result *big.Rat
	for _, p := range latest.Accrued {
		if p.Date.UTC().Truncate(24*time.Hour).Equal(day) && p.Value != nil {
			v, ok := new(big.Rat).SetString(*p.Value)
			if !ok || v.Sign() < 0 || result != nil && result.Cmp(v) != 0 {
				return nil
			}
			unit := p.ValueCurrency
			// GetAccruedInterests uses a currency-less Quotation. Nominal's
			// currency does not establish its unit, particularly for replacement
			// bonds settled in RUB. Only corroborate today's exact amount with
			// the explicitly denominated current ACI; never carry it backwards.
			if unit == "" && day.Equal(latest.AsOf.UTC().Truncate(24*time.Hour)) && latest.Terms.CurrentAccrued != nil {
				current := latest.Terms.CurrentAccrued
				amount, valid := new(big.Rat).SetString(current.Amount)
				if valid && amount.Cmp(v) == 0 {
					unit = current.Currency
				}
			}
			if unit == "" || unit != currency {
				return nil
			}
			result = v
		}
	}
	return result
}

const trackerInstrumentBasisSQL = `WITH listings AS (SELECT DISTINCT provider,scope,id::uuid instrument_id FROM jsonb_to_recordset($2::jsonb) AS x(provider text,scope text,id text))
SELECT jsonb_build_array(
 (SELECT jsonb_build_array(count(*),COALESCE(sum(o.revision),0),max(o.received_at)) FROM market_provider_observations o JOIN listings l ON l.provider=o.provider AND l.scope=o.access_scope AND l.instrument_id=o.instrument_id WHERE o.tenant_id=$1 AND o.kind='daily_candle' AND o.observed_at<(date_trunc('day',$3::timestamptz AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AND o.observed_at>=$3::timestamptz-interval '3666 days'),
 (SELECT jsonb_agg(to_jsonb(s) ORDER BY s.provider,s.access_scope) FROM market_data_sources s WHERE s.tenant_id=$1 AND EXISTS(SELECT 1 FROM listings l WHERE l.provider=s.provider AND l.scope=s.access_scope)),
 (SELECT jsonb_build_array(count(*),max(b.recorded_at)) FROM market_bond_snapshots b JOIN listings l ON l.provider=b.provider AND l.scope=b.access_scope AND l.instrument_id=b.instrument_id WHERE b.tenant_id=$1 AND b.from_at<=$3 AND b.to_at>=$3::timestamptz-interval '3666 days'))`

func trackerCardBondExcerpt(input *md.SavedBondFacts, now time.Time) (*md.SavedBondFacts, bool) {
	if input == nil {
		return nil, false
	}
	out := *input
	out.Coupons = nil
	out.Events = nil
	out.Accrued = nil
	limited := false
	lower, upper := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -365), now.UTC().Truncate(24*time.Hour).AddDate(0, 0, 365)
	for _, p := range input.Coupons {
		if p.Date.Before(lower) || p.Date.After(upper) {
			continue
		}
		if len(out.Coupons) < 200 {
			out.Coupons = append(out.Coupons, p)
		} else {
			limited = true
		}
	}
	for _, p := range input.Events {
		if p.Date.Before(lower) || p.Date.After(upper) {
			continue
		}
		if len(out.Events) < 200 {
			out.Events = append(out.Events, p)
		} else {
			limited = true
		}
	}
	for _, p := range input.Accrued {
		if p.Date.Before(lower) || p.Date.After(now) {
			continue
		}
		if len(out.Accrued) < 200 {
			out.Accrued = append(out.Accrued, p)
		} else {
			limited = true
		}
	}
	return &out, limited
}

func readTrackerBondInfo(ctx context.Context, tx pgx.Tx, scope md.Scope, id string, at time.Time) (*md.SavedBondFacts, error) {
	var f md.SavedBondFacts
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT b.job_id,b.data,b.payload_hash,b.recorded_at FROM market_bond_snapshots b JOIN market_data_sources s USING(tenant_id,provider,access_scope) WHERE b.tenant_id=$1 AND b.provider=$2 AND b.access_scope=$3 AND b.instrument_id=$4 AND s.enabled AND b.recorded_at<=$5 ORDER BY b.recorded_at DESC,b.job_id DESC LIMIT 1`, scope.TenantID, scope.Provider, scope.AccessScope, id, at).Scan(&f.ID, &raw, &f.Hash, &f.RecordedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if json.Unmarshal(raw, &f.BondFacts) != nil || f.Hash != investmentHash(f.BondFacts) || f.Instrument.ID != id || f.Instrument.Kind != "bond" {
		return nil, d.ErrInvestmentPrecondition
	}
	if _, e = md.NormalizeBondFacts(f.BondFacts); e != nil {
		return nil, d.ErrInvestmentPrecondition
	}
	if f.AsOf.After(at) {
		return nil, nil
	}
	return &f, nil
}
