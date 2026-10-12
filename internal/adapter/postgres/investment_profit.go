package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"math/big"
	"time"
)

func (r *MarketDataRepo) ProfitForSeries(ctx context.Context, tenant, actor, key, from, to string) (nav.ProfitAnalytics, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nav.ProfitAnalytics{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, e := readInvestmentProfit(ctx, tx, tenant, actor, key, from, to)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func readInvestmentProfit(ctx context.Context, tx pgx.Tx, tenant, actor, key, from, to string) (nav.ProfitAnalytics, error) {
	var out nav.ProfitAnalytics
	performance, e := readInvestmentPerformance(ctx, tx, tenant, actor, key, from, to)
	if e != nil {
		return out, e
	}
	out.Performance = performance
	out.Timeline, e = calc.CalculateProfitTimeline(calc.PeriodPerformanceInput{Days: performance.Days, FlowCoverage: true, ResetDates: performance.ResetDates})
	if e != nil {
		return out, e
	}
	q, _, _, e := readNAVRegisteredRequest(ctx, tx, tenant, actor, key)
	if e != nil {
		return out, e
	}
	through, e := nav.DayCutoff(to)
	if e != nil {
		return out, e
	}
	if through.After(time.Now()) {
		return out, d.ErrInvestmentInvalid
	}
	start, _ := time.Parse(time.DateOnly, from)
	baseDate := start.AddDate(0, 0, -1).Format(time.DateOnly)
	baseline, _ := nav.DayCutoff(baseDate)
	policy, points, e := readFXSnapshot(ctx, tx, tenant, q.FXPolicyKey, through, time.Time{})
	if e != nil && !errors.Is(e, md.ErrNotFound) {
		return out, e
	}
	fixings := []calc.FXFixing{}
	for _, p := range points {
		fixings = append(fixings, p.Fixing())
	}
	policyRef := q.FXPolicyKey
	if e == nil {
		policyRef = md.FXPolicyRef(policy.Key, policy.Version)
	}
	cache := map[string]*calc.FXResolver{}
	loc, _ := time.LoadLocation("Europe/Moscow")
	convert := func(currency string, amount *big.Rat, at time.Time) (*big.Rat, string, error) {
		if amount == nil {
			return nil, "book_value_unknown", nil
		}
		if amount.Sign() == 0 || currency == q.Currency {
			return new(big.Rat).Set(amount), "", nil
		}
		date := at.In(loc).Format(time.DateOnly)
		resolver := cache[date]
		if resolver == nil {
			cutoff, err := nav.DayCutoff(date)
			if err != nil {
				return nil, "", err
			}
			resolver, err = calc.NewFXResolver(fixings, policyRef, cutoff)
			if err != nil {
				return nil, "", err
			}
			cache[date] = resolver
		}
		rate, ok := resolver.Resolve(currency, q.Currency)
		if !ok {
			return nil, "fx_missing", nil
		}
		return new(big.Rat).Mul(amount, rate.Rate), "", nil
	}
	kinds := []string{"realized_clean", "realized_accrued", "unrealized_clean_change", "unrealized_accrued_change", "earned_income", "fees", "taxes", "cash_fx", "trade_fees_in_cost"}
	hasSecurityFlow := false
	for _, flow := range performance.Flows {
		if flow.Kind == "security_transfer_in" || flow.Kind == "security_transfer_out" {
			hasSecurityFlow = true
		}
	}
	if hasSecurityFlow {
		kinds = append(kinds, "security_transfer_carry")
	}
	components := map[string]*calc.ProfitComponent{}
	knownCounts := map[string]int{}
	for _, kind := range kinds {
		components[kind] = &calc.ProfitComponent{Kind: kind, Known: new(big.Rat), Complete: true, Informational: kind == "trade_fees_in_cost"}
	}
	add := func(kind, currency string, amount *big.Rat, at time.Time, reason string) error {
		c := components[kind]
		v, why, err := convert(currency, amount, at)
		if err != nil {
			return err
		}
		if reason != "" || why != "" {
			c.Complete = false
			if c.Reason == "" {
				c.Reason = reason
				if c.Reason == "" {
					c.Reason = why
				}
			}
		}
		if v != nil {
			knownCounts[kind]++
			c.Known.Add(c.Known, v)
		}
		return nil
	}
	contributions := nav.NewContributionBuilder()
	if hasSecurityFlow {
		connected, err := readConnectedInvestmentReplay(ctx, tx, tenant, q.LedgerIDs, nil, through, nil)
		if err != nil {
			return out, err
		}
		for _, flow := range performance.Flows {
			if flow.Kind != "security_transfer_in" && flow.Kind != "security_transfer_out" {
				continue
			}
			reason := flow.Reason
			if flow.Value == nil && reason == "" {
				reason = "fx_missing"
			}
			var movement *big.Rat
			if flow.Value != nil {
				movement = new(big.Rat).Neg(flow.Value)
			}
			contributions.Add(flow.InstrumentKey, flow.LedgerID, "cash_movements", movement, reason)
			receipt, ok := connected.Transfers[flow.TransferID]
			if !ok {
				return out, d.ErrInvestmentPrecondition
			}
			cost := new(big.Rat)
			for _, lot := range receipt.Lots {
				if lot.CleanCost == nil || lot.AcquiredAccrued == nil {
					cost = nil
					break
				}
				cost.Add(cost, lot.CleanCost)
				cost.Add(cost, lot.AcquiredAccrued)
			}
			converted, why, err := convert(receipt.Currency, cost, flow.At)
			if err != nil {
				return out, err
			}
			if reason == "" {
				reason = why
			}
			var adjustment *big.Rat
			if converted != nil && flow.Value != nil {
				if flow.Kind == "security_transfer_out" {
					converted.Neg(converted)
				}
				adjustment = new(big.Rat).Sub(converted, flow.Value)
			}
			if err = add("security_transfer_carry", q.Currency, adjustment, flow.At, reason); err != nil {
				return out, err
			}
		}
	}

	values := [2]nav.Result{}
	for i, at := range []time.Time{baseline, through} {
		boundaryRequest := q
		boundaryRequest.At = at
		values[i], e = readInvestmentValuation(ctx, tx, boundaryRequest)
		if e != nil {
			return out, e
		}
	}
	// Aggregate economic cash movements by date/currency before FX. Relabelling
	// receivables/payables on settlement cancels and requires no extra FX fixing.
	type cashDate struct{ date, currency string }
	movements := map[cashDate]*big.Rat{}
	for _, id := range q.LedgerIDs {
		ledger, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, tenant, id))
		if e != nil {
			return out, e
		}
		ops, e := loadInvestmentOperations(ctx, tx, tenant, id)
		if e != nil {
			return out, e
		}
		endReplay, e := replayInvestmentLedgerTx(ctx, tx, tenant, ledger, ops, through)
		if e != nil {
			return out, e
		}
		// Closed positions and payment-only keys remain part of attribution.
		operationsByID := map[string]d.InvestmentOperation{}
		for _, op := range ops {
			operationsByID[op.ID] = op
		}
		startReplay := inv.ReplayResult{}
		baselineKnown := !baseline.Before(ledger.StartedAt)
		if baselineKnown {
			startReplay, e = replayInvestmentLedgerTx(ctx, tx, tenant, ledger, ops, baseline)
			if e != nil {
				return out, e
			}
		}
		keys := map[string]bool{}
		for _, position := range startReplay.Snapshot.Positions {
			keys[position.InstrumentKey] = true
		}
		for _, position := range endReplay.Snapshot.Positions {
			keys[position.InstrumentKey] = true
			quantity, ok := new(big.Rat).SetString(position.Quantity)
			if !ok {
				return out, d.ErrInvestmentPrecondition
			}
			contributions.Ensure(position.InstrumentKey, ledger.ID, quantity.Sign() > 0)
		}
		for _, op := range ops {
			if op.Status == "posted" && op.InstrumentKey != "" && op.OccurredAt.After(baseline) && !op.OccurredAt.After(through) {
				keys[op.InstrumentKey] = true
			}
		}
		for instrument := range keys {
			contributions.Ensure(instrument, ledger.ID, false)
			for i, replay := range []inv.ReplayResult{startReplay, endReplay} {
				kind := "closing_market"
				if i == 0 {
					kind = "opening_market"
				}
				if i == 0 && !baselineKnown {
					contributions.Add(instrument, ledger.ID, kind, nil, "ledger_history_missing")
					continue
				}
				present := false
				var quantity *big.Rat
				for _, position := range replay.Snapshot.Positions {
					if position.InstrumentKey == instrument {
						present = true
						quantity, _ = new(big.Rat).SetString(position.Quantity)
						break
					}
				}
				if !present || quantity != nil && quantity.Sign() == 0 {
					contributions.Add(instrument, ledger.ID, kind, new(big.Rat), "")
					continue
				}
				componentID := fmt.Sprintf("%d:%s/position/%s", len(ledger.ID), ledger.ID, instrument)
				amount := values[i].ComponentValues[componentID]
				why := ""
				for _, issue := range values[i].Issues {
					if issue.ComponentID == componentID || issue.ComponentID == ledger.ID || issue.ComponentID == "" {
						why = issue.Code
						break
					}
				}
				if amount != nil {
					amount = new(big.Rat).Set(amount)
					if i == 0 {
						amount.Neg(amount)
					}
				}
				contributions.Add(instrument, ledger.ID, kind, amount, why)
			}
		}
		// Net economic cash legs per operation/date/currency before conversion:
		// settlement only changes buckets and therefore requires no extra fixing.
		type instrumentCashKey struct{ instrument, date, currency string }
		instrumentCash := map[instrumentCashKey]*big.Rat{}
		for _, leg := range endReplay.CashLegs {
			op := operationsByID[leg.OperationID]
			if op.InstrumentKey == "" || !leg.EffectiveAt.After(baseline) || leg.EffectiveAt.After(through) {
				continue
			}
			amount, ok := new(big.Rat).SetString(leg.Amount)
			if !ok {
				return out, d.ErrInvestmentPrecondition
			}
			if leg.Bucket == "payable" {
				amount.Neg(amount)
			}
			k := instrumentCashKey{op.InstrumentKey, leg.EffectiveAt.In(loc).Format(time.DateOnly), leg.Currency}
			if instrumentCash[k] == nil {
				instrumentCash[k] = new(big.Rat)
			}
			instrumentCash[k].Add(instrumentCash[k], amount)
		}
		for k, amount := range instrumentCash {
			at, _ := nav.DayCutoff(k.date)
			converted, why, err := convert(k.currency, amount, at)
			if err != nil {
				return out, err
			}
			contributions.Add(k.instrument, ledger.ID, "cash_movements", converted, why)
		}
		for _, op := range ops {
			if op.Status == "posted" && op.OccurredAt.After(baseline) && !op.OccurredAt.After(through) {
				if op.Kind == "distribution" && op.Distribution != nil {
					var amount *big.Rat
					if op.Distribution.MarketValue != "" {
						var ok bool
						amount, ok = new(big.Rat).SetString(op.Distribution.MarketValue)
						if !ok {
							return out, d.ErrInvestmentPrecondition
						}
					}
					value, reason, err := convert(op.Currency, amount, op.OccurredAt)
					if err != nil {
						return out, err
					}
					contributions.ApplyDistribution(op, value, reason)
				}
				contributions.RestrictOperation(op)
			}
		}
		for _, event := range endReplay.ProfitEvents {
			if event.At.After(baseline) {
				if e = add(event.Kind, event.Currency, event.Amount, event.At, event.Reason); e != nil {
					return out, e
				}
			}
		}
		for _, op := range ops {
			if op.Status == "posted" && op.TaxReviewRequired && op.OccurredAt.After(baseline) && !op.OccurredAt.After(through) {
				components["taxes"].Complete = false
				components["taxes"].Reason = "tax_review_required"
			}
		}
		for _, leg := range endReplay.CashLegs {
			if !leg.EffectiveAt.After(baseline) || leg.EffectiveAt.After(through) {
				continue
			}
			v, ok := new(big.Rat).SetString(leg.Amount)
			if !ok {
				return out, d.ErrInvestmentPrecondition
			}
			if leg.Bucket == "payable" {
				v.Neg(v)
			}
			key := cashDate{leg.EffectiveAt.In(loc).Format(time.DateOnly), leg.Currency}
			if movements[key] == nil {
				movements[key] = new(big.Rat)
			}
			movements[key].Add(movements[key], v)
		}
		for i, at := range []time.Time{baseline, through} {
			sign := int64(1)
			if i == 0 {
				sign = -1
			}
			if at.Before(ledger.StartedAt) {
				for _, kind := range []string{"unrealized_clean_change", "unrealized_accrued_change", "cash_fx"} {
					components[kind].Complete = false
					components[kind].Reason = "ledger_history_missing"
				}
				continue
			}
			replay := endReplay
			if i == 0 {
				replay = startReplay
			}
			for _, cash := range replay.Snapshot.Cash {
				amount, ok := new(big.Rat).SetString(cash.Economic)
				if !ok {
					return out, d.ErrInvestmentPrecondition
				}
				amount.Mul(amount, big.NewRat(sign, 1))
				if e = add("cash_fx", cash.Currency, amount, at, ""); e != nil {
					return out, e
				}
			}
			for _, position := range replay.Snapshot.Positions {
				id := fmt.Sprintf("%d:%s/position/%s", len(ledger.ID), ledger.ID, position.InstrumentKey)
				native := values[i].Positions[id]
				quantity, validQuantity := new(big.Rat).SetString(position.Quantity)
				if !validQuantity {
					return out, d.ErrInvestmentPrecondition
				}
				book := replay.ExactPositions[position.InstrumentKey]
				for _, part := range []struct {
					kind         string
					cost, market *big.Rat
				}{{"unrealized_clean_change", book.CleanCost, native.Clean}, {"unrealized_accrued_change", book.AcquiredAccrued, native.Accrued}} {
					market := part.market
					if quantity != nil && quantity.Sign() == 0 {
						market = new(big.Rat)
					}
					var amount *big.Rat
					cost := part.cost
					if market != nil && cost != nil && (quantity.Sign() == 0 || native.Currency == position.Currency) {
						amount = new(big.Rat).Sub(market, cost)
						amount.Mul(amount, big.NewRat(sign, 1))
					}
					if e = add(part.kind, position.Currency, amount, at, ""); e != nil {
						return out, e
					}
				}
			}
		}
	}
	for key, amount := range movements {
		at, _ := nav.DayCutoff(key.date)
		if e = add("cash_fx", key.currency, new(big.Rat).Neg(amount), at, ""); e != nil {
			return out, e
		}
	}
	ordered := []calc.ProfitComponent{}
	for _, kind := range kinds {
		if !components[kind].Complete && knownCounts[kind] == 0 {
			components[kind].Known = nil
		}
		ordered = append(ordered, *components[kind])
	}
	total := performance.Profit
	if performance.Reason != "" {
		total = nil
	}
	out.Instruments = contributions.Finish(total)
	if len(out.Instruments.Rows) > 10000 {
		return out, d.ErrInvestmentLimit
	}
	for i := range out.Instruments.Rows {
		out.Instruments.Rows[i].Metadata, e = readPositionMetadata(ctx, tx, tenant, out.Instruments.Rows[i].InstrumentKey, through)
		if e != nil {
			return out, e
		}
	}
	out.Attribution, e = calc.ReconcileProfit(performance.Profit, ordered)
	return out, e
}
