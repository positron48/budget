package postgres

import (
	"context"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"math/big"
	"sort"
	"time"
)

func readTrackerContributions(ctx context.Context, tenant string, q nav.TrackerHistoryRequest, inputs map[string]investmentReplayInput, market map[string]trackerHistoryMarket, splits map[string]map[string][]d.InvestmentOperation, fixings []calc.FXFixing, policy string, after, through, readAt time.Time, baseline *nav.TrackerHistoryPoint, points []nav.TrackerHistoryPoint, classification *nav.InstrumentGroups) ([]nav.TrackerContribution, error) {
	if baseline == nil || len(points) != 1 || classification == nil {
		return nil, d.ErrInvestmentPrecondition
	}
	end := points[0]
	groups, names, wanted := map[string]string{}, map[string]string{}, map[string]bool{}
	for _, g := range classification.Groups {
		names[g.ID] = g.Name
	}
	for _, m := range classification.Memberships {
		groups[m.LedgerID+":"+m.InstrumentKey] = m.GroupID
	}
	for _, id := range q.GroupIDs {
		wanted[id] = true
	}
	rows := map[string]*nav.TrackerContribution{}
	keys := map[string]bool{}
	add := func(ledger, key string) *nav.TrackerContribution {
		id := ledger + ":" + key
		group := groups[id]
		if len(wanted) > 0 && key != "" && !wanted[group] {
			return nil
		}
		if r := rows[id]; r != nil {
			return r
		}
		if key != "" {
			keys[key] = true
		}
		r := &nav.TrackerContribution{InScope: len(wanted) == 0 || key != "", LedgerID: ledger, LedgerName: inputs[ledger].Ledger.Name, InstrumentKey: key, GroupID: group, GroupName: names[group], Flow: new(big.Rat), OpeningQuantity: new(big.Rat), ClosingQuantity: new(big.Rat), Opening: new(big.Rat), Closing: new(big.Rat)}
		for _, boundary := range []struct {
			point   nav.TrackerHistoryPoint
			opening bool
		}{{*baseline, true}, {end, false}} {
			if v, ok := boundary.point.Components[id]; ok {
				if boundary.opening {
					r.Opening, r.OpeningQuantity = v.Value, v.Quantity
				} else {
					r.Closing, r.ClosingQuantity = v.Value, v.Quantity
				}
				if v.Value == nil {
					r.Reason = "valuation_missing"
				}
			}
			if reason := boundary.point.LedgerReasons[ledger]; reason != "" {
				r.Reason = reason
			}
		}
		rows[id] = r
		return r
	}
	for _, ledger := range q.LedgerIDs {
		input := inputs[ledger]
		// Retain closed holdings and income-only instruments from the recorded journal.
		for _, op := range input.Operations {
			if op.Status != "posted" || op.OccurredAt.After(through) {
				continue
			}
			switch op.Kind {
			case "buy", "sell", "dividend", "coupon", "fee", "repo_income", "cash_interest", "amortization", "redemption", "opening_position", "security_transfer_in", "security_transfer_out", "split", "distribution":
				add(ledger, op.InstrumentKey)
			}
			if op.Distribution != nil {
				add(ledger, op.Distribution.TargetInstrumentKey)
			}
		}
		for id := range baseline.Components {
			if len(id) > len(ledger) && id[:len(ledger)+1] == ledger+":" {
				add(ledger, id[len(ledger)+1:])
			}
		}
		for id := range end.Components {
			if len(id) > len(ledger) && id[:len(ledger)+1] == ledger+":" {
				add(ledger, id[len(ledger)+1:])
			}
		}
		flows, e := trackerConvertedStrategyFlows(ctx, tenant, []string{ledger}, inputs, market, splits, fixings, policy, q.Currency, after, through, readAt)
		if e != nil {
			return nil, e
		}
		for _, f := range flows {
			if !f.At.After(after) || f.At.After(through) {
				continue
			}
			r := add(ledger, f.InstrumentKey)
			if r == nil {
				continue
			}
			if f.Value == nil {
				r.Flow = nil
				r.Reason = f.Reason
				if r.Reason == "" {
					r.Reason = "strategy_flow_unknown"
				}
			} else if r.Flow != nil {
				r.Flow.Add(r.Flow, f.Value)
			}
		}
		// A cost-allocation record alone is not transfer-day market-value evidence.
		// Do not manufacture per-security gains at a cross-instrument distribution.
		for _, op := range input.Operations {
			if op.Status == "posted" && op.Kind == "distribution" && op.Distribution != nil && op.OccurredAt.After(after) && !op.OccurredAt.After(through) {
				for _, key := range []string{op.InstrumentKey, op.Distribution.TargetInstrumentKey} {
					if r := add(ledger, key); r != nil {
						r.Flow = nil
						r.Reason = "corporate_allocation_market_value_unknown"
					}
				}
			}
		}
	}
	if len(keys) > 500 || len(rows) > 10020 {
		return nil, d.ErrInvestmentLimit
	}
	ids := []string{}
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]nav.TrackerContribution, 0, len(ids))
	for _, id := range ids {
		r := rows[id]
		if r.Reason == "" && r.Flow != nil && r.Opening != nil && r.Closing != nil {
			r.Profit = new(big.Rat).Sub(new(big.Rat).Sub(r.Closing, r.Opening), r.Flow)
		}
		result = append(result, *r)
	}
	return result, nil
}
