package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type trackerHistoryMarket struct {
	mapping      md.Mapping
	observations map[string][]md.ScopedObservation
	bonds        map[string][]md.SavedBondFacts
	series       map[string]*md.PriceSeries
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
	release, e := r.acquireTrackerHistorySlot(ctx)
	if e != nil {
		return out, e
	}
	defer release()

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
	var classification *nav.InstrumentGroups
	groupFor := map[string]string{}
	wantedGroups := map[string]bool{}
	if len(q.GroupIDs) > 0 || q.ContributionOnly {
		groups, err := readInstrumentGroups(ctx, tx, tenant)
		if err != nil {
			return out, err
		}
		classification = &groups
		out.ClassificationVersion = groups.Version
		valid := map[string]bool{"": true}
		for _, g := range groups.Groups {
			valid[g.ID] = true
		}
		for _, id := range q.GroupIDs {
			if !valid[id] {
				return out, d.ErrInvestmentPrecondition
			}
			wantedGroups[id] = true
		}
		for _, m := range groups.Memberships {
			groupFor[m.LedgerID+"\x00"+m.InstrumentKey] = m.GroupID
		}
	}
	wantedInstruments := map[string]bool{}
	for _, key := range q.InstrumentKeys {
		wantedInstruments[key] = true
	}
	selected := func(ledger, key string) bool {
		return (len(q.GroupIDs) == 0 || wantedGroups[groupFor[ledger+"\x00"+key]]) && (len(wantedInstruments) == 0 || wantedInstruments[key])
	}
	if out.Generation, e = investmentGenerationTx(ctx, tx, tenant); e != nil {
		return out, e
	}
	through, e := nav.DayCutoff(q.To)
	if e != nil {
		return out, e
	}
	policy, points, e := readFXSnapshot(ctx, tx, tenant, q.FXPolicyKey, through, time.Time{})
	if e != nil {
		return out, e
	}
	eligiblePoints := points[:0]
	for _, p := range points {
		if !p.AvailableAt.After(through) {
			eligiblePoints = append(eligiblePoints, p)
		}
	}
	points = eligiblePoints
	out.FXPolicyVersion, out.FXMode = policy.Version, policy.Mode
	fixings := make([]calc.FXFixing, 0, len(points))
	for _, p := range points {
		fixings = append(fixings, p.Fixing())
	}
	fxSeries, e := calc.NewFXSeries(fixings, md.FXPolicyRef(policy.Key, policy.Version))
	if e != nil {
		return out, e
	}

	inputs := map[string]investmentReplayInput{}
	startsFromZero := map[string]bool{}
	scopedOperations := map[string][]d.InvestmentOperation{}
	splits := map[string]map[string][]d.InvestmentOperation{}
	count := 0
	for _, id := range q.LedgerIDs {
		l, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, tenant, id))
		if e != nil {
			return out, investmentError(e)
		}
		if !l.PortfolioTracking {
			return out, d.ErrInvestmentPrecondition
		}
		var ops []d.InvestmentOperation
		if q.BasisOnly {
			rows, err := tx.Query(ctx, `SELECT DISTINCT COALESCE(data->>'InstrumentKey',''),COALESCE(data#>>'{Distribution,TargetInstrumentKey}',''),status FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2`, tenant, id)
			if err != nil {
				return out, err
			}
			for rows.Next() {
				var key, target, status string
				if err = rows.Scan(&key, &target, &status); err != nil {
					rows.Close()
					return out, err
				}
				op := d.InvestmentOperation{LedgerID: id, InstrumentKey: key, Status: status}
				if target != "" {
					op.Distribution = &d.InvestmentDistribution{TargetInstrumentKey: target}
				}
				ops = append(ops, op)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return out, err
			}
		} else {
			ops, e = loadInvestmentOperations(ctx, tx, tenant, id)
		}
		if e != nil {
			return out, e
		}
		count += len(ops)
		if count > 100000 {
			return out, d.ErrInvestmentLimit
		}
		inputs[id] = investmentReplayInput{Ledger: l, Operations: ops}
		scopeOps := ops
		if len(q.InstrumentKeys) > 0 || len(q.GroupIDs) > 0 {
			scopeOps = nil
			for _, op := range ops {
				if selected(id, op.InstrumentKey) || (op.Distribution != nil && selected(id, op.Distribution.TargetInstrumentKey)) {
					scopeOps = append(scopeOps, op)
				}
			}
		}
		scopedOperations[id] = scopeOps
		startsFromZero[id] = inv.RecordedTrackerStartsFromZero(l, scopeOps)
		splits[id] = map[string][]d.InvestmentOperation{}
		for _, op := range ops {
			if op.Kind == "split" && op.Status == "posted" {
				splits[id][op.InstrumentKey] = append(splits[id][op.InstrumentKey], op)
			}
		}
	}
	if len(q.InstrumentKeys) > 0 {
		known := map[string]bool{}
		for _, input := range inputs {
			for _, op := range input.Operations {
				if op.Status != "posted" {
					continue
				}
				known[op.InstrumentKey] = true
				if op.Distribution != nil {
					known[op.Distribution.TargetInstrumentKey] = true
				}
			}
		}
		for _, key := range q.InstrumentKeys {
			if !known[key] {
				return out, d.ErrInvestmentPrecondition
			}
		}
	}
	// Journal heads and immutable provider revision counters fence pagination.
	// Intraday last-price refreshes must neither mix corrected daily candles nor
	// continuously invalidate an otherwise unchanged historical chart.
	var cache json.RawMessage
	basisKeys := map[string]bool{}
	for _, input := range inputs {
		for _, op := range input.Operations {
			if op.InstrumentKey != "" && selected(op.LedgerID, op.InstrumentKey) {
				basisKeys[op.InstrumentKey] = true
			}
			if op.Distribution != nil && selected(op.LedgerID, op.Distribution.TargetInstrumentKey) {
				basisKeys[op.Distribution.TargetInstrumentKey] = true
			}
		}
	}
	identities := []string{}
	for key := range basisKeys {
		identities = append(identities, key)
	}
	sort.Strings(identities)
	analyticsKeys := map[string]bool{}
	for _, input := range inputs {
		for _, op := range input.Operations {
			if op.Status != "posted" {
				continue
			}
			if op.InstrumentKey != "" && selected(op.LedgerID, op.InstrumentKey) {
				analyticsKeys[op.InstrumentKey] = true
			}
			if op.Distribution != nil && selected(op.LedgerID, op.Distribution.TargetInstrumentKey) {
				analyticsKeys[op.Distribution.TargetInstrumentKey] = true
			}
		}
	}
	for key := range analyticsKeys {
		out.AnalyticsInstrumentKeys = append(out.AnalyticsInstrumentKeys, key)
	}
	slices.Sort(out.AnalyticsInstrumentKeys)
	// Unknown source identities remain account-wide blockers. Known pending
	// instruments can be excluded from a separate, explicitly scoped calculation.
	pendingRows, err := tx.Query(ctx, selectedPendingTrackerTradesSQL, tenant, q.LedgerIDs)
	if err != nil {
		return out, err
	}
	for pendingRows.Next() {
		var ledger, date, key string
		if err = pendingRows.Scan(&ledger, &date, &key); err != nil {
			pendingRows.Close()
			return out, err
		}
		if len(date) >= 10 && date[:10] <= q.To && analyticsKeys[key] && selected(ledger, key) {
			out.AnalyticsExcludedKeys = append(out.AnalyticsExcludedKeys, key)
			out.MonetaryExcludedKeys = append(out.MonetaryExcludedKeys, key)
		}
	}
	err = pendingRows.Err()
	pendingRows.Close()
	if err != nil {
		return out, err
	}
	collectExclusions := func() {
		if out.Baseline != nil {
			out.AnalyticsExcludedKeys = append(out.AnalyticsExcludedKeys, out.Baseline.AnalyticsExcludedKeys...)
			out.MonetaryExcludedKeys = append(out.MonetaryExcludedKeys, out.Baseline.MonetaryExcludedKeys...)
		}
		for _, point := range out.Points {
			out.AnalyticsExcludedKeys = append(out.AnalyticsExcludedKeys, point.AnalyticsExcludedKeys...)
			out.MonetaryExcludedKeys = append(out.MonetaryExcludedKeys, point.MonetaryExcludedKeys...)
		}
		slices.Sort(out.AnalyticsExcludedKeys)
		out.AnalyticsExcludedKeys = slices.Compact(out.AnalyticsExcludedKeys)
		slices.Sort(out.MonetaryExcludedKeys)
		out.MonetaryExcludedKeys = slices.Compact(out.MonetaryExcludedKeys)
	}

	if e = tx.QueryRow(ctx, trackerHistoryBasisSQL, tenant, identities, q.LedgerIDs, through).Scan(&cache); e != nil {
		return out, e
	}
	heads := []d.InvestmentLedger{}
	for _, id := range q.LedgerIDs {
		heads = append(heads, inputs[id].Ledger)
	}
	out.BasisKey = investmentHash(struct {
		ValuationRules string
		Heads          []d.InvestmentLedger
		Policy         md.SavedFXPolicy
		Points         []md.SavedFXPoint
		Cache          json.RawMessage
		Classification *nav.InstrumentGroups
		GroupIDs       []string
		InstrumentKeys []string
	}{"daily-eod-v3", heads, policy, points, cache, classification, q.GroupIDs, q.InstrumentKeys})
	if q.BasisOnly {
		return out, tx.Commit(ctx)
	}
	first, _ := time.Parse(time.DateOnly, q.From)
	performanceBaseline := ""
	if q.PerformanceFrom != "" {
		base, _ := time.Parse(time.DateOnly, q.PerformanceFrom)
		performanceBaseline = base.AddDate(0, 0, -1).Format(time.DateOnly)
	}
	last, _ := time.Parse(time.DateOnly, q.To)
	end := first.AddDate(0, 0, nav.TrackerPageDays(q.PageDays)-1)
	if end.After(last) {
		end = last
	} else if end.Before(last) {
		out.NextDate = end.AddDate(0, 0, 1).Format(time.DateOnly)
	}
	calculationFirst := first
	if q.PerformanceFrom == q.From || q.ContributionOnly {
		calculationFirst = first.AddDate(0, 0, -1)
	}
	if q.ContributionOnly {
		end = last
		out.NextDate = ""
	}
	if q.DetailDate != "" {
		calculationFirst, _ = time.Parse(time.DateOnly, q.DetailDate)
		end = calculationFirst
		out.NextDate = ""
	}
	cacheKey := ""
	if !q.ContributionOnly && q.DetailDate == "" {
		cacheKey = investmentHash(struct {
			Tenant, Actor, Basis, Currency, Through, PerformanceFrom string
			Performance, MonetaryOnly, CompactIssues                 bool
		}{tenant, actor, out.BasisKey, q.Currency, q.To, q.PerformanceFrom, q.PerformanceFrom != "", q.MonetaryOnly, q.CompactIssues})
		cached := []nav.TrackerHistoryPoint{}
		for day := calculationFirst; !day.After(end); day = day.AddDate(0, 0, 1) {
			point, ok := r.trackerHistoryCache.get(cacheKey+day.Format(time.DateOnly), out.ReadAt)
			if !ok || point.Date != day.Format(time.DateOnly) {
				cached = nil
				break
			}
			cached = append(cached, point)
		}
		if len(cached) > 0 {
			for _, point := range cached {
				if point.Date < q.From {
					copy := point
					out.Baseline = &copy
				} else {
					out.Points = append(out.Points, point)
				}
			}
			collectExclusions()
			return out, tx.Commit(ctx)
		}
	}

	captureComponents := q.ContributionOnly || q.DetailDate != ""
	days := []time.Time{}
	states := [][]d.InvestmentSnapshot{}
	allKeys := map[string]bool{}
	for day := calculationFirst; !day.After(end); day = day.AddDate(0, 0, 1) {
		at, err := nav.DayCutoff(day.Format(time.DateOnly))
		if err != nil {
			return out, err
		}
		if !q.ContributionOnly || day.Equal(calculationFirst) || day.Equal(end) {
			days = append(days, at)
		}
	}
	unitHistory := map[string][]d.InvestmentSnapshot{}
	needsReplay := false
	for _, id := range q.LedgerIDs {
		input := inputs[id]
		history, supported, err := inv.TrackerUnitHistory(input.Ledger, scopedOperations[id], days)
		if err != nil {
			return out, err
		}
		if supported {
			unitHistory[id] = history
		} else {
			needsReplay = true
		}
	}
	if needsReplay {
		// Unit projections are committed atomically with the journal and source
		// corrections. Reading them avoids repeating connected FIFO for each day.
		rows, err := tx.Query(ctx, `SELECT ledger_id::text,operation_id::text,instrument_key,quantity::text,effective_at FROM investment_security_legs WHERE tenant_id=$1 AND ledger_id=ANY($2::uuid[]) ORDER BY ledger_id,effective_at,ordinal LIMIT 100001`, tenant, q.LedgerIDs)
		if err != nil {
			return out, err
		}
		legs := map[string][]d.InvestmentSecurityLeg{}
		count := 0
		for rows.Next() {
			var id string
			var leg d.InvestmentSecurityLeg
			if err = rows.Scan(&id, &leg.OperationID, &leg.InstrumentKey, &leg.Quantity, &leg.EffectiveAt); err != nil {
				rows.Close()
				return out, err
			}
			legs[id] = append(legs[id], leg)
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if count > 100000 {
			return out, d.ErrInvestmentLimit
		}
		needsReplay = false
		for _, id := range q.LedgerIDs {
			if _, ok := unitHistory[id]; ok {
				continue
			}
			input := inputs[id]
			history, supported, err := inv.TrackerSecurityLegHistory(input.Ledger, scopedOperations[id], legs[id], days)
			if err != nil {
				return out, err
			}
			if supported {
				unitHistory[id] = history
			} else {
				needsReplay = true
			}
		}
	}
	var pendingFrom map[string]string
	if len(q.GroupIDs) > 0 || len(q.InstrumentKeys) > 0 {
		pendingFrom, e = readSelectedPendingTrackerTradeDates(ctx, tx, tenant, q.LedgerIDs, selected)
	} else {
		pendingFrom, e = readPendingTrackerTradeDates(ctx, tx, tenant, q.LedgerIDs)
	}
	if e != nil {
		return out, e
	}
	moscow, e := time.LoadLocation("Europe/Moscow")
	if e != nil {
		return out, e
	}
	for i, at := range days {
		var replay inv.PortfolioReplay
		if needsReplay {
			replay, e = readConnectedInvestmentReplay(ctx, tx, tenant, q.LedgerIDs, inputs, at, nil)
			if e != nil {
				return out, e
			}
		}
		snapshots := []d.InvestmentSnapshot{}
		for _, id := range q.LedgerIDs {
			var s d.InvestmentSnapshot
			if history, ok := unitHistory[id]; ok {
				s = history[i]
			} else {
				s = replay.Ledgers[id].Snapshot
			}
			if len(q.GroupIDs) > 0 || len(q.InstrumentKeys) > 0 {
				filtered := []d.InvestmentPosition{}
				for _, p := range s.Positions {
					if selected(id, p.InstrumentKey) {
						filtered = append(filtered, p)
					}
				}
				s.Positions = filtered
			}
			for j := range s.Positions {
				s.Positions[j].Lots = nil
			}
			if pending, ok := pendingFrom[id]; ok && pending <= at.In(moscow).Format("2006-01-02 15:04:05") {
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
		states = append(states, snapshots)
	}
	if q.PerformanceFrom != "" || q.ContributionOnly {
		for _, input := range inputs {
			for _, op := range input.Operations {
				if op.Status == "posted" && selected(op.LedgerID, op.InstrumentKey) && (op.Kind == "security_transfer_in" || op.Kind == "security_transfer_out") && !op.OccurredAt.Before(days[0].Add(-24*time.Hour)) && !op.OccurredAt.After(days[len(days)-1]) {
					allKeys[op.InstrumentKey] = true
				}
			}
		}
	}
	if len(allKeys) > 500 {
		return out, d.ErrInvestmentLimit
	}
	keys := []string{}
	for key := range allKeys {
		keys = append(keys, key)
	}
	sparse := []time.Time{}
	if q.ContributionOnly {
		sparse = append(sparse, days...)
		seen := map[string]bool{}
		for _, input := range inputs {
			for _, op := range input.Operations {
				if op.Status == "posted" && (op.Kind == "security_transfer_in" || op.Kind == "security_transfer_out") && op.OccurredAt.After(days[0]) && !op.OccurredAt.After(days[len(days)-1]) {
					date := op.OccurredAt.In(moscow).Format(time.DateOnly)
					if !seen[date] {
						at, _ := nav.DayCutoff(date)
						sparse = append(sparse, at)
						seen[date] = true
					}
				}
			}
		}
	}
	market, e := readTrackerHistoryMarket(ctx, tx, tenant, keys, days[0], days[len(days)-1], out.ReadAt, sparse...)
	if e != nil {
		return out, e
	}
	var strategy map[string]trackerStrategyDay
	if q.PerformanceFrom != "" {
		if len(q.GroupIDs) > 0 || len(q.InstrumentKeys) > 0 {
			strategy, e = trackerSelectionFlows(ctx, tenant, q, inputs, market, splits, fixings, md.FXPolicyRef(policy.Key, policy.Version), days[0].Add(-24*time.Hour), days[len(days)-1], out.ReadAt, selected)
		} else {
			strategy, e = trackerStrategyFlows(ctx, tenant, q.LedgerIDs, inputs, market, splits, fixings, md.FXPolicyRef(policy.Key, policy.Version), q.Currency, days[0].Add(-24*time.Hour), days[len(days)-1], out.ReadAt)
		}
		if e != nil {
			return out, e
		}
	}
	for i, at := range days {
		row := nav.TrackerHistoryPoint{Date: at.In(moscow).Format(time.DateOnly), State: "ready"}
		if captureComponents {
			row.Components = map[string]nav.TrackerContributionValue{}
			row.LedgerReasons = map[string]string{}
		}
		components := []calc.ExactComponent{}
		required, priced := map[string]int{}, map[string]int{}
		amounts := map[string]calc.ExactComponent{}
		quotes := map[string]nav.PortfolioQuote{}
		details := map[string]*nav.TrackerValueDetail{}
		for _, s := range states[i] {
			if !startsFromZero[s.Ledger.ID] && at.Before(s.Ledger.StartedAt) {
				row.Issues = append(row.Issues, calc.QualityIssue{ComponentID: s.Ledger.ID, Code: "ledger_history_missing"})
				if captureComponents {
					row.LedgerReasons[s.Ledger.ID] = "ledger_history_missing"
				}
			}
			for _, code := range s.Issues {
				switch code {
				case "date_only_timing":
					continue // A known civil date determines end-of-day units without an intraday timestamp.
				case "mixed_native_cost_currencies", "unknown_acquisition_cost", "unknown_acquired_interest", "historical_acquisition_date_unknown", "coupon_allocation_required", "history_before_opening_unknown", "settlement_assumed_trade_time", "imported_cash_deficit_reviewed":
					continue // Cost/cash assumptions do not make known security units unpriced.
				}
				row.Issues = append(row.Issues, calc.QualityIssue{ComponentID: s.Ledger.ID, Code: code})
				if captureComponents {
					row.LedgerReasons[s.Ledger.ID] = code
				}
			}
			for _, p := range s.Positions {
				quantity, _ := new(big.Rat).SetString(p.Quantity)
				if quantity.Sign() == 0 {
					continue
				}
				required[p.InstrumentKey]++
				quote, ok := quotes[p.InstrumentKey]
				if !ok {
					quote, e = trackerHistoricalQuote(tenant, p.InstrumentKey, market[p.InstrumentKey], at, out.ReadAt)
					if e != nil {
						return out, e
					}
					quotes[p.InstrumentKey] = quote
				}
				id := s.Ledger.ID + ":" + p.InstrumentKey
				if captureComponents {
					row.Components[id] = nav.TrackerContributionValue{Quantity: new(big.Rat).Set(quantity)}
				}
				if q.DetailDate != "" {
					detail := &nav.TrackerValueDetail{LedgerID: s.Ledger.ID, LedgerName: s.Ledger.Name, InstrumentKey: p.InstrumentKey, Quantity: new(big.Rat).Set(quantity), Currency: quote.Currency, Source: quote.Source, PriceAt: quote.At, MappingVersion: quote.MappingVersion, Issues: append([]string{}, quote.Issues...)}
					if detail.Currency == "" {
						detail.Currency = p.Currency
					}
					details[id] = detail
				}
				if !nav.UsablePortfolioQuote(quote) {
					for _, issue := range quote.Issues {
						row.Issues = append(row.Issues, calc.QualityIssue{ComponentID: id, Code: issue})
					}
					continue
				}
				price, err := nav.RebaseHistoricalPrice(quote.CleanPrice, quote.At, at, p.InstrumentKey, splits[s.Ledger.ID][p.InstrumentKey])
				if err != nil {
					return out, err
				}
				amount := new(big.Rat).Mul(quantity, price)
				if detail := details[id]; detail != nil {
					detail.Price = new(big.Rat).Set(price)
					detail.NativeValue = new(big.Rat).Set(amount)
				}
				componentKey := p.InstrumentKey
				if captureComponents {
					componentKey = id
				}
				if existing, ok := amounts[componentKey]; ok {
					if existing.Currency != quote.Currency {
						return out, d.ErrInvestmentPrecondition
					}
					amount.Add(amount, existing.Amount)
				}
				amounts[componentKey] = calc.ExactComponent{ID: componentKey, Currency: quote.Currency, Amount: amount}
				priced[p.InstrumentKey]++
			}
		}
		row.Positions = len(required)
		for _, amount := range amounts {
			components = append(components, amount)
		}
		sort.Slice(components, func(i, j int) bool { return components[i].ID < components[j].ID })
		rates, e := fxSeries.At(at)
		if e != nil {
			return out, e
		}
		value, e := calc.ValueExactUsingResolver(components, rates, q.Currency)
		if e != nil {
			return out, e
		}
		row.KnownValue = value.KnownValue
		row.Issues = append(row.Issues, value.Issues...)
		converted := map[string]int{}
		for key, v := range value.ComponentValues {
			if captureComponents {
				c := row.Components[key]
				c.Value = v
				row.Components[key] = c
				_, instrument, _ := strings.Cut(key, ":")
				if v != nil {
					converted[instrument]++
				}
				continue
			}
			if v != nil && priced[key] == required[key] {
				row.PricedPositions++
			}
		}
		if captureComponents {
			for key, n := range required {
				if converted[key] == n {
					row.PricedPositions++
				}
			}
		}
		if q.DetailDate != "" {
			row.DetailFX = value.FXPaths
			wantedFixings := map[string]bool{}
			for _, legs := range value.FXPaths {
				for _, leg := range legs {
					wantedFixings[leg.ID+":"+fmt.Sprint(leg.Revision)] = true
				}
			}
			for _, fixing := range fixings {
				if wantedFixings[fixing.ID+":"+fmt.Sprint(fixing.Revision)] {
					row.DetailFixings = append(row.DetailFixings, fixing)
				}
			}
			ids := make([]string, 0, len(details))
			for id := range details {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				detail := details[id]
				detail.Value = row.Components[id].Value
				if detail.NativeValue != nil && detail.Value != nil {
					if detail.NativeValue.Sign() != 0 {
						detail.FXRate = new(big.Rat).Quo(detail.Value, detail.NativeValue)
					} else if detail.Currency == q.Currency {
						detail.FXRate = big.NewRat(1, 1)
					}
				}
				for _, issue := range row.Issues {
					if issue.ComponentID == id || issue.ComponentID == detail.LedgerID {
						detail.Issues = append(detail.Issues, issue.Code)
					}
				}
				row.Details = append(row.Details, *detail)
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
		if q.PerformanceFrom != "" {
			flow := strategy[row.Date]
			row.StrategyFlow = new(big.Rat)
			row.FlowReason = flow.reason
			for _, key := range flow.excludedKeys {
				if basisKeys[key] {
					row.AnalyticsExcludedKeys = append(row.AnalyticsExcludedKeys, key)
					row.MonetaryExcludedKeys = append(row.MonetaryExcludedKeys, key)
				}
			}
			row.Reset = flow.reset
			if flow.value != nil {
				row.StrategyFlow.Set(flow.value)
			}
			for _, issue := range row.Issues {
				if issue.Code == "source_trades_pending" {
					row.FlowReason = "source_trades_pending"
				}
			}
			if row.FlowReason != "" {
				row.StrategyFlow = nil
			}
		}

		// Temporary historical price gaps remain gaps in the full calculation.
		// Only unresolved boundary holdings (and unknown flows above) exclude a security
		// from the fixed preliminary subset; one stale holiday must not discard
		// seven years of a bond's coupons, trades and valuations.
		for _, issue := range row.Issues {
			key := issue.ComponentID
			if _, instrument, found := strings.Cut(key, ":"); found && !basisKeys[key] {
				key = instrument
			}
			if basisKeys[key] {
				row.AnalyticsExcludedKeys = append(row.AnalyticsExcludedKeys, key)
				if row.Date == q.To || row.Date == performanceBaseline {
					row.MonetaryExcludedKeys = append(row.MonetaryExcludedKeys, key)
				}
			}
		}
		slices.Sort(row.AnalyticsExcludedKeys)
		row.AnalyticsExcludedKeys = slices.Compact(row.AnalyticsExcludedKeys)
		row.IssueCount = len(row.Issues)
		if q.CompactIssues {
			seen := map[string]bool{}
			compact := []calc.QualityIssue{}
			for _, issue := range row.Issues {
				if !seen[issue.Code] {
					seen[issue.Code] = true
					compact = append(compact, calc.QualityIssue{Code: issue.Code})
				}
			}
			row.Issues = compact
		}

		if row.Date < q.From {
			out.Baseline = &row
		} else {
			out.Points = append(out.Points, row)
		}
	}
	if q.ContributionOnly {
		out.Contributions, e = readTrackerContributions(ctx, tenant, q, inputs, market, splits, fixings, md.FXPolicyRef(policy.Key, policy.Version), days[0], days[len(days)-1], out.ReadAt, out.Baseline, out.Points, classification)
		if e != nil {
			return out, e
		}
	}
	collectExclusions()
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	if cacheKey != "" {
		if out.Baseline != nil {
			r.trackerHistoryCache.put(cacheKey+out.Baseline.Date, *out.Baseline, out.ReadAt)
		}
		for _, point := range out.Points {
			r.trackerHistoryCache.put(cacheKey+point.Date, point, out.ReadAt)
		}
	}
	return out, nil
}

func readTrackerHistoryMarket(ctx context.Context, tx pgx.Tx, tenant string, keys []string, from, to, readAt time.Time, sparse ...time.Time) (map[string]trackerHistoryMarket, error) {
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
	// Fetch the mapped series in one bounded statement. The transaction already
	// selects latest corrected history; no historical knowledge cutoff is used.
	return readTrackerHistorySeries(ctx, tx, tenant, result, from, to, readAt, sparse...)
}

func historyPriceSelection(tenant string, p md.MappingPeriod, at time.Time) md.PriceSelectionRequest {
	return md.PriceSelectionRequest{Scope: md.Scope{TenantID: tenant, Provider: p.Provider, AccessScope: p.AccessScope}, InstrumentID: p.InstrumentID, Kind: "daily_candle", SourceKind: "exchange", Adjustment: "unadjusted", At: at, Retrospective: &md.RetrospectiveDailyPolicy{Convention: md.RetrospectiveCloseNextUTCDay, MaxAge: nav.PortfolioQuoteMaxAge}}
}

func trackerHistoricalQuote(tenant, key string, entry trackerHistoryMarket, at, readAt time.Time) (nav.PortfolioQuote, error) {
	if estimate, ok := nav.EstimatedPortfolioQuote(entry.mapping, at); ok {
		return estimate, nil
	}
	missing := func(code string) nav.PortfolioQuote {
		return nav.WithPortfolioFallback(entry.mapping, nav.PortfolioQuote{InstrumentKey: key, State: "unavailable", Issues: []string{code}}, at)
	}
	if entry.mapping.InstrumentKey == "" {
		return missing("mapping_missing"), nil
	}
	for _, p := range entry.mapping.HistoryPeriods() {
		if !at.Before(p.From) && (p.To.IsZero() || at.Before(p.To)) {
			cache := historyPeriodKey(p)
			price, e := entry.selectPrice(historyPriceSelection(tenant, p, at), cache)
			if e != nil {
				return nav.PortfolioQuote{}, e
			}
			if price.Fact == nil && len(entry.observations[cache+"/unknown"]) > 0 {
				selection := historyPriceSelection(tenant, p, at)
				selection.Adjustment = "provider_unspecified"
				price, e = entry.selectPrice(selection, cache+"/unknown")
				if e != nil {
					return nav.PortfolioQuote{}, e
				}
			}
			if price.Fact != nil && price.Fact.EffectiveAt.Before(p.From) {
				return missing("price_before_mapping"), nil
			}
			var bond *md.SavedBondFacts
			fact := price.Fact
			if fact == nil && price.State == "stale" {
				fact = price.Candidate
			}
			if fact != nil {
				for _, observation := range entry.observations[cache] {
					if observation.ID == price.ObservationID && observation.Revision == price.ObservationRevision {
						var err error
						bond, err = md.ObservationBondFacts(observation.SavedObservation)
						if err != nil {
							return nav.PortfolioQuote{}, err
						}
						break
					}
				}
				day := fact.EffectiveAt.UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
				for _, f := range entry.bonds[cache] {
					if bond == nil && !day.Before(f.From) && !day.After(f.To) {
						copy := f
						bond = &copy
						break
					}
				}
			}
			quote, err := nav.NormalizeHistoricalPortfolioQuote(entry.mapping, price, at, readAt, bond)
			if err != nil {
				return nav.PortfolioQuote{}, err
			}
			return nav.WithPortfolioFallback(entry.mapping, quote, at), nil
		}
	}
	return missing("terms_missing"), nil
}

// All pages share the requested upper bound and ten-year horizon. Neither a
// newly fetched unrelated paper nor today's close can invalidate yesterday's
// chart. Corrections inside that horizon and pending-source changes still do.
const trackerHistoryBasisSQL = `WITH mappings AS (
 SELECT instrument_key,data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=ANY($2::text[])
), listings AS (
 SELECT DISTINCT p->>'Provider' provider,p->>'AccessScope' access_scope,(p->>'InstrumentID')::uuid instrument_id
 FROM mappings CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_array_length(COALESCE(data->'HistoricalPeriods','[]'::jsonb))>0 THEN data->'HistoricalPeriods' ELSE COALESCE(NULLIF(data->'Periods','null'::jsonb),'[]'::jsonb) END) p
), bindings AS (
 SELECT * FROM investment_import_ledger_bindings WHERE tenant_id=$1 AND ledger_id=ANY($3::uuid[])
), source_rows AS (
 SELECT r.* FROM investment_import_rows r JOIN bindings b USING(tenant_id,import_id,account_id)
)
SELECT jsonb_build_array(
 (SELECT jsonb_build_array(count(*),COALESCE(sum(o.revision),0),max(o.received_at))
 FROM market_provider_observations o JOIN listings l USING(provider,access_scope,instrument_id)
 WHERE o.tenant_id=$1 AND o.kind='daily_candle' AND o.source_kind='exchange'
 AND o.observed_at<date_trunc('day',$4::timestamptz AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
 AND o.observed_at>=$4::timestamptz-interval '3666 days'),
 (SELECT jsonb_build_array(count(*),max(b.recorded_at)) FROM market_bond_snapshots b JOIN listings l USING(provider,access_scope,instrument_id)
 WHERE b.tenant_id=$1 AND b.from_at<=$4 AND b.to_at>=$4::timestamptz-interval '3666 days'),
 (SELECT jsonb_agg(data ORDER BY instrument_key) FROM mappings),
 (SELECT jsonb_agg(to_jsonb(s) ORDER BY s.provider,s.access_scope) FROM market_data_sources s
 WHERE s.tenant_id=$1 AND EXISTS(SELECT 1 FROM listings l WHERE l.provider=s.provider AND l.access_scope=s.access_scope)),
 (SELECT jsonb_agg(jsonb_build_array(import_id,record,version) ORDER BY import_id,record) FROM source_rows),
 (SELECT jsonb_agg(to_jsonb(b) ORDER BY import_id,account_id) FROM bindings b),
 (SELECT jsonb_agg(to_jsonb(l) ORDER BY l.import_id,l.record) FROM investment_source_links l
 WHERE l.tenant_id=$1 AND EXISTS(SELECT 1 FROM source_rows r WHERE r.import_id=l.import_id AND r.record=l.record))
 )`

func (entry trackerHistoryMarket) selectPrice(q md.PriceSelectionRequest, key string) (md.SelectedPrice, error) {
	// Retain the preceding observation across holidays and suspended trading.
	// Eligibility still rejects future, incomplete and contradictory candles.
	if series := entry.series[key]; series != nil {
		return series.Select(q)
	}
	return md.SelectValuationPrice(q, entry.observations[key])
}
