package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type trackerHistoryMarket struct {
	mapping      md.Mapping
	observations map[string][]md.ScopedObservation
	bonds        map[string][]md.SavedBondFacts
}

func historyPeriodKey(p md.MappingPeriod) string {
	return p.Provider + "/" + p.AccessScope + "/" + p.InstrumentID
}

// One snapshot includes the journal, corrected candles, entitlements and FX.
// The existing worker hydrates mapped open/closed positions; this read neither
// contacts a provider nor writes financial facts or legacy NAV projections.
func (r *MarketDataRepo) TrackerHistory(ctx context.Context, tenant, actor string, input nav.TrackerHistoryRequest) (nav.TrackerHistory, error) {
	out := nav.TrackerHistory{ReadAt: time.Now().UTC().Truncate(time.Microsecond)}
	q, e := nav.NormalizeTrackerHistory(input, out.ReadAt)
	if e != nil {
		return out, e
	}
	out.Currency, out.FXPolicyKey = q.Currency, q.FXPolicyKey
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
	if out.Generation, e = investmentGenerationTx(ctx, tx, tenant); e != nil {
		return out, e
	}
	policy, points, e := readFXSnapshot(ctx, tx, tenant, q.FXPolicyKey, out.ReadAt, time.Time{})
	if e != nil {
		return out, e
	}
	out.FXPolicyVersion, out.FXMode = policy.Version, policy.Mode
	fixings := make([]calc.FXFixing, 0, len(points))
	for _, p := range points {
		fixings = append(fixings, p.Fixing())
	}
	inputs := map[string]investmentReplayInput{}
	count := 0
	for _, id := range q.LedgerIDs {
		l, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, tenant, id))
		if e != nil {
			return out, investmentError(e)
		}
		if !l.PortfolioTracking {
			return out, d.ErrInvestmentPrecondition
		}
		ops, e := loadInvestmentOperations(ctx, tx, tenant, id)
		if e != nil {
			return out, e
		}
		count += len(ops)
		if count > 100000 {
			return out, d.ErrInvestmentLimit
		}
		inputs[id] = investmentReplayInput{Ledger: l, Operations: ops}
	}
	// Journal heads and immutable provider revision counters fence pagination.
	// Intraday last-price refreshes must neither mix corrected daily candles nor
	// continuously invalidate an otherwise unchanged historical chart.
	var cache json.RawMessage
	if e = tx.QueryRow(ctx, trackerHistoryBasisSQL, tenant).Scan(&cache); e != nil {
		return out, e
	}
	heads := []d.InvestmentLedger{}
	for _, id := range q.LedgerIDs {
		heads = append(heads, inputs[id].Ledger)
	}
	out.BasisKey = investmentHash(struct {
		Heads  []d.InvestmentLedger
		Policy md.SavedFXPolicy
		Points []md.SavedFXPoint
		Cache  json.RawMessage
	}{heads, policy, points, cache})
	first, _ := time.Parse(time.DateOnly, q.From)
	last, _ := time.Parse(time.DateOnly, q.To)
	end := first.AddDate(0, 0, 30)
	if end.After(last) {
		end = last
	} else if end.Before(last) {
		out.NextDate = end.AddDate(0, 0, 1).Format(time.DateOnly)
	}
	days := []time.Time{}
	states := [][]d.InvestmentSnapshot{}
	allKeys := map[string]bool{}
	for day := first; !day.After(end); day = day.AddDate(0, 0, 1) {
		at, err := nav.DayCutoff(day.Format(time.DateOnly))
		if err != nil {
			return out, err
		}
		replay, e := readConnectedInvestmentReplay(ctx, tx, tenant, q.LedgerIDs, inputs, at, nil)
		if e != nil {
			return out, e
		}
		snapshots := []d.InvestmentSnapshot{}
		for _, id := range q.LedgerIDs {
			s := replay.Ledgers[id].Snapshot
			// The chart retains units, not 31 copies of every FIFO lot.
			for j := range s.Positions {
				s.Positions[j].Lots = nil
			}
			pending, e := pendingPortfolioTrades(ctx, tx, tenant, id, at)
			if e != nil {
				return out, e
			}
			if pending {
				s.Issues = append(s.Issues, "source_trades_pending")
			}
			snapshots = append(snapshots, s)
			for _, p := range s.Positions {
				v, ok := new(big.Rat).SetString(p.Quantity)
				if !ok || v.Sign() < 0 {
					return out, d.ErrInvestmentPrecondition
				}
				if v.Sign() > 0 {
					allKeys[p.InstrumentKey] = true
				}
			}
		}
		days = append(days, at)
		states = append(states, snapshots)
	}
	if len(allKeys) > 500 {
		return out, d.ErrInvestmentLimit
	}
	keys := []string{}
	for key := range allKeys {
		keys = append(keys, key)
	}
	market, e := readTrackerHistoryMarket(ctx, tx, tenant, keys, days[0], days[len(days)-1], out.ReadAt)
	if e != nil {
		return out, e
	}
	for i, at := range days {
		row := nav.TrackerHistoryPoint{Date: first.AddDate(0, 0, i).Format(time.DateOnly), State: "ready"}
		components := []calc.ExactComponent{}
		for _, s := range states[i] {
			if at.Before(s.Ledger.StartedAt) {
				row.Issues = append(row.Issues, calc.QualityIssue{ComponentID: s.Ledger.ID, Code: "ledger_history_missing"})
			}
			for _, code := range s.Issues {
				switch code {
				case "mixed_native_cost_currencies", "unknown_acquisition_cost", "unknown_acquired_interest", "historical_acquisition_date_unknown", "coupon_allocation_required", "history_before_opening_unknown", "settlement_assumed_trade_time", "imported_cash_deficit_reviewed":
					continue // Cost/cash assumptions do not make known security units unpriced.
				}
				row.Issues = append(row.Issues, calc.QualityIssue{ComponentID: s.Ledger.ID, Code: code})
			}
			for _, p := range s.Positions {
				quantity, _ := new(big.Rat).SetString(p.Quantity)
				if quantity.Sign() == 0 {
					continue
				}
				row.Positions++
				quote, e := trackerHistoricalQuote(tenant, p.InstrumentKey, market[p.InstrumentKey], at, out.ReadAt)
				if e != nil {
					return out, e
				}
				id := s.Ledger.ID + ":" + p.InstrumentKey
				if quote.CleanPrice == nil || quote.State != "fresh" {
					for _, issue := range quote.Issues {
						row.Issues = append(row.Issues, calc.QualityIssue{ComponentID: id, Code: issue})
					}
					continue
				}
				components = append(components, calc.ExactComponent{ID: id, Currency: quote.Currency, Amount: new(big.Rat).Mul(quantity, quote.CleanPrice)})
			}
		}
		value, e := calc.ValueExact(components, fixings, q.Currency, md.FXPolicyRef(policy.Key, policy.Version), at)
		if e != nil {
			return out, e
		}
		row.KnownValue = value.KnownValue
		row.Issues = append(row.Issues, value.Issues...)
		for _, v := range value.ComponentValues {
			if v != nil {
				row.PricedPositions++
			}
		}
		if len(row.Issues) > 0 || row.PricedPositions != row.Positions || !value.Complete {
			row.State = "incomplete"
		}
		issueKeys := []string{}
		for _, issue := range row.Issues {
			issueKeys = append(issueKeys, issue.ComponentID+"/"+issue.Code)
		}
		sort.Strings(issueKeys)
		row.CoverageKey = investmentHash(issueKeys)
		out.Points = append(out.Points, row)
	}
	return out, tx.Commit(ctx)
}

func readTrackerHistoryMarket(ctx context.Context, tx pgx.Tx, tenant string, keys []string, from, to, readAt time.Time) (map[string]trackerHistoryMarket, error) {
	result := map[string]trackerHistoryMarket{}
	rows, e := tx.Query(ctx, `SELECT instrument_key,data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=ANY($2::text[])`, tenant, keys)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var key string
		var raw []byte
		var m md.Mapping
		if e = rows.Scan(&key, &raw); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(raw, &m); e != nil {
			rows.Close()
			return nil, e
		}
		if m.InstrumentKey != key || m.Version < 1 {
			rows.Close()
			return nil, d.ErrInvestmentPrecondition
		}
		result[key] = trackerHistoryMarket{mapping: m, observations: map[string][]md.ScopedObservation{}, bonds: map[string][]md.SavedBondFacts{}}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	// Each listing is loaded once for the page, not once per historical day.
	for key, entry := range result {
		for _, p := range entry.mapping.Periods {
			if p.From.After(to) || !p.To.IsZero() && !p.To.After(from) {
				continue
			}
			cache := historyPeriodKey(p)
			if _, ok := entry.observations[cache]; ok {
				continue
			}
			selection := historyPriceSelection(tenant, p, to)
			observations, e := readPriceEvidenceMode(ctx, tx, selection, from.UTC().Truncate(24*time.Hour).Add(-5*24*time.Hour), false)
			if e != nil {
				return nil, e
			}
			entry.observations[cache] = observations
			selection.Adjustment = "provider_unspecified"
			unknown, e := readPriceEvidenceMode(ctx, tx, selection, from.UTC().Truncate(24*time.Hour).Add(-5*24*time.Hour), false)
			if e != nil {
				return nil, e
			}
			entry.observations[cache+"/unknown"] = unknown
			if p.Kind != "bond" || p.Unit != "percent_of_nominal" {
				continue
			}
			rows, e := tx.Query(ctx, `SELECT b.job_id,b.data,b.payload_hash,b.recorded_at FROM market_bond_snapshots b JOIN market_data_sources s USING(tenant_id,provider,access_scope) WHERE b.tenant_id=$1 AND b.provider=$2 AND b.access_scope=$3 AND b.instrument_id=$4 AND s.enabled AND b.from_at<=$5 AND b.to_at>=$6 AND b.recorded_at<=$7 ORDER BY b.recorded_at DESC,b.job_id DESC LIMIT 1001`, tenant, p.Provider, p.AccessScope, p.InstrumentID, to, from.Add(-5*24*time.Hour), readAt)
			if e != nil {
				return nil, e
			}
			for rows.Next() {
				var f md.SavedBondFacts
				var raw []byte
				if e = rows.Scan(&f.ID, &raw, &f.Hash, &f.RecordedAt); e != nil {
					rows.Close()
					return nil, e
				}
				if json.Unmarshal(raw, &f.BondFacts) != nil || f.Hash != investmentHash(f.BondFacts) {
					rows.Close()
					return nil, d.ErrInvestmentPrecondition
				}
				if _, e = md.NormalizeBondFacts(f.BondFacts); e != nil {
					rows.Close()
					return nil, e
				}
				entry.bonds[cache] = append(entry.bonds[cache], f)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return nil, e
			}
			if len(entry.bonds[cache]) > 1000 {
				return nil, d.ErrInvestmentLimit
			}
		}
		result[key] = entry
	}
	return result, nil
}
func historyPriceSelection(tenant string, p md.MappingPeriod, at time.Time) md.PriceSelectionRequest {
	return md.PriceSelectionRequest{Scope: md.Scope{TenantID: tenant, Provider: p.Provider, AccessScope: p.AccessScope}, InstrumentID: p.InstrumentID, Kind: "daily_candle", SourceKind: "exchange", Adjustment: "unadjusted", At: at, Retrospective: &md.RetrospectiveDailyPolicy{Convention: md.RetrospectiveCloseNextUTCDay, MaxAge: nav.PortfolioQuoteMaxAge}}
}
func trackerHistoricalQuote(tenant, key string, entry trackerHistoryMarket, at, readAt time.Time) (nav.PortfolioQuote, error) {
	missing := func(code string) nav.PortfolioQuote {
		return nav.PortfolioQuote{InstrumentKey: key, State: "unavailable", Issues: []string{code}}
	}
	if entry.mapping.InstrumentKey == "" {
		return missing("mapping_missing"), nil
	}
	for _, p := range entry.mapping.Periods {
		if !at.Before(p.From) && (p.To.IsZero() || at.Before(p.To)) {
			cache := historyPeriodKey(p)
			price, e := md.SelectValuationPrice(historyPriceSelection(tenant, p, at), entry.observations[cache])
			if e != nil {
				return nav.PortfolioQuote{}, e
			}
			if price.Fact == nil && len(entry.observations[cache+"/unknown"]) > 0 {
				selection := historyPriceSelection(tenant, p, at)
				selection.Adjustment = "provider_unspecified"
				price, e = md.SelectValuationPrice(selection, entry.observations[cache+"/unknown"])
				if e != nil {
					return nav.PortfolioQuote{}, e
				}
			}
			if price.Fact != nil && price.Fact.EffectiveAt.Before(p.From) {
				return missing("price_before_mapping"), nil
			}
			var bond *md.SavedBondFacts
			if price.Fact != nil {
				day := price.Fact.EffectiveAt.UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
				for _, f := range entry.bonds[cache] {
					if !day.Before(f.From) && !day.After(f.To) {
						copy := f
						bond = &copy
						break
					}
				}
			}
			return nav.NormalizeHistoricalPortfolioQuote(entry.mapping, price, at, readAt, bond)
		}
	}
	return missing("terms_missing"), nil
}

const trackerHistoryBasisSQL = `SELECT jsonb_build_array(
 (SELECT jsonb_build_array(count(*),COALESCE(sum(revision),0),max(received_at)) FROM market_provider_observations WHERE tenant_id=$1 AND kind='daily_candle'),
 (SELECT jsonb_build_array(count(*),max(recorded_at)) FROM market_bond_snapshots WHERE tenant_id=$1),
 (SELECT jsonb_agg(data ORDER BY instrument_key) FROM investment_market_mappings WHERE tenant_id=$1),
 (SELECT jsonb_agg(jsonb_build_array(provider,access_scope,enabled) ORDER BY provider,access_scope) FROM market_data_sources WHERE tenant_id=$1),
 (SELECT jsonb_build_array(count(*),COALESCE(sum(version),0)) FROM investment_import_rows WHERE tenant_id=$1),
 (SELECT jsonb_agg(to_jsonb(b) ORDER BY import_id,account_id) FROM investment_import_ledger_bindings b WHERE tenant_id=$1)
 )`
