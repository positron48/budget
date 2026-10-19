package investment

import (
	"math/big"
	"slices"
	"strings"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

// Up to 93 chart dates plus one baseline share a single journal pass.
const MaxTrackerUnitCutoffs = 94

// TrackerUnitHistory evaluates only security units for a standalone portfolio.
// It uses exact arithmetic until the same presentation boundary as Replay.
// Cash-linked and corporate-transfer books return supported=false and retain
// their connected replay contract. This must not be used for cost or profit.
func TrackerUnitHistory(ledger d.InvestmentLedger, input []d.InvestmentOperation, cutoffs []time.Time) (snapshots []d.InvestmentSnapshot, supported bool, err error) {
	if !ledger.PortfolioTracking || !nativePortfolioHistory(input) {
		return nil, false, nil
	}
	if ledger.ID == "" || ledger.TenantID == "" || ledger.StartedAt.IsZero() || len(cutoffs) == 0 {
		return nil, true, d.ErrInvestmentInvalid
	}
	if len(input) > MaxOperations || len(cutoffs) > MaxTrackerUnitCutoffs {
		return nil, true, d.ErrInvestmentLimit
	}
	for i, at := range cutoffs {
		if at.IsZero() || i > 0 && !at.After(cutoffs[i-1]) {
			return nil, true, d.ErrInvestmentInvalid
		}
	}
	ops := slices.Clone(input)
	for i := range ops {
		if ops[i].Status != "voided" {
			if e := ValidateOperation(&ops[i]); e != nil {
				return nil, true, e
			}
		}
	}
	slices.SortFunc(ops, func(a, b d.InvestmentOperation) int {
		if c := a.OccurredAt.Compare(b.OccurredAt); c != 0 {
			return c
		}
		if x, y := operationTimingPriority(a), operationTimingPriority(b); x != y {
			if x < y {
				return -1
			}
			return 1
		}
		if a.Sequence < b.Sequence {
			return -1
		}
		if a.Sequence > b.Sequence {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	quantities := map[string]*big.Rat{}
	redeemed := map[string]bool{}
	ids, sequences := map[string]bool{}, map[int64]bool{}
	startsFromZero := RecordedTrackerStartsFromZero(ledger, input)
	appendSnapshot := func(at time.Time) {
		s := d.InvestmentSnapshot{Ledger: ledger, AsOf: at, Positions: []d.InvestmentPosition{}}
		if startsFromZero || !at.Before(ledger.StartedAt) {
			keys := []string{}
			for key, q := range quantities {
				if q.Sign() > 0 {
					keys = append(keys, key)
				}
			}
			slices.Sort(keys)
			for _, key := range keys {
				s.Positions = append(s.Positions, d.InvestmentPosition{InstrumentKey: key, Quantity: decimalString(quantities[key])})
			}
		}
		snapshots = append(snapshots, s)
	}
	for _, o := range ops {
		if o.Status == "voided" {
			continue
		}
		if o.Status != "posted" || o.ID == "" || ids[o.ID] || o.Sequence < 1 || sequences[o.Sequence] || o.LedgerID != ledger.ID || o.TenantID != ledger.TenantID {
			return nil, true, invalid("unique scoped portfolio operation required")
		}
		ids[o.ID], sequences[o.Sequence] = true, true
		if o.ImportedCashDeficitReason != "" {
			if e := validateCashDeficitSource(o); e != nil {
				return nil, true, e
			}
		}
		for len(snapshots) < len(cutoffs) && o.OccurredAt.After(cutoffs[len(snapshots)]) {
			appendSnapshot(cutoffs[len(snapshots)])
		}
		if o.InstrumentKey == "" {
			continue
		}
		q := quantities[o.InstrumentKey]
		if q == nil {
			q = new(big.Rat)
			quantities[o.InstrumentKey] = q
		}
		switch o.Kind {
		case "buy", "opening_position":
			if redeemed[o.InstrumentKey] {
				return nil, true, inconsistent("cannot buy a redeemed instrument")
			}
			q.Add(q, rational(o.Quantity))
		case "sell", "redemption":
			amount := rational(o.Quantity)
			if amount.Cmp(q) > 0 {
				return nil, true, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: inconsistent("short sale is not supported")}
			}
			if o.Kind == "redemption" {
				if amount.Cmp(q) != 0 {
					return nil, true, inconsistent("full redemption must close the entire position")
				}
				redeemed[o.InstrumentKey] = true
			}
			q.Sub(q, amount)
		case "split":
			q.Mul(q, new(big.Rat).Quo(rational(o.SplitNew), rational(o.SplitOld)))
		}
	}
	// Future operations are validated as well, exactly as in the full replay.
	for len(snapshots) < len(cutoffs) {
		appendSnapshot(cutoffs[len(snapshots)])
	}
	return snapshots, true, nil
}

// TrackerSecurityLegHistory projects daily units from validated security legs.
// Legs are written atomically with journal mutations and already include exact
// transfer, split and distribution deltas. This projection is only for market
// valuation, never acquisition costs or profit. Legacy cash books keep replay.
func TrackerSecurityLegHistory(ledger d.InvestmentLedger, ops []d.InvestmentOperation, input []d.InvestmentSecurityLeg, cutoffs []time.Time) ([]d.InvestmentSnapshot, bool, error) {
	if !ledger.PortfolioTracking {
		return nil, false, nil
	}
	for _, o := range ops {
		if o.Status == "voided" {
			continue
		}
		switch o.Kind {
		case "buy", "sell", "opening_position", "split", "redemption", "dividend", "coupon", "repo_income", "cash_interest", "fee", "tax", "tax_refund", "deposit", "withdrawal", "opening_cash", "security_transfer_in", "security_transfer_out", "distribution", "amortization":
		default:
			return nil, false, nil
		}
	}
	if len(cutoffs) == 0 || len(cutoffs) > MaxTrackerUnitCutoffs {
		return nil, true, d.ErrInvestmentLimit
	}
	legs := slices.Clone(input)
	slices.SortStableFunc(legs, func(a, b d.InvestmentSecurityLeg) int { return a.EffectiveAt.Compare(b.EffectiveAt) })
	quantities := map[string]*big.Rat{}
	out := make([]d.InvestmentSnapshot, 0, len(cutoffs))
	next := 0
	startsFromZero := RecordedTrackerStartsFromZero(ledger, ops)
	for i, at := range cutoffs {
		if at.IsZero() || i > 0 && !at.After(cutoffs[i-1]) {
			return nil, true, d.ErrInvestmentInvalid
		}
		for next < len(legs) && !legs[next].EffectiveAt.After(at) {
			leg := legs[next]
			amount, ok := new(big.Rat).SetString(leg.Quantity)
			if !ok {
				return nil, true, d.ErrInvestmentInvalid
			}
			if quantities[leg.InstrumentKey] == nil {
				quantities[leg.InstrumentKey] = new(big.Rat)
			}
			quantities[leg.InstrumentKey].Add(quantities[leg.InstrumentKey], amount)
			next++
		}
		s := d.InvestmentSnapshot{Ledger: ledger, AsOf: at, Positions: []d.InvestmentPosition{}}
		if startsFromZero || !at.Before(ledger.StartedAt) {
			keys := []string{}
			for key, q := range quantities {
				if q.Sign() < 0 {
					return nil, true, d.ErrInvestmentPrecondition
				}
				if q.Sign() > 0 {
					keys = append(keys, key)
				}
			}
			slices.Sort(keys)
			for _, key := range keys {
				s.Positions = append(s.Positions, d.InvestmentPosition{InstrumentKey: key, Quantity: decimalString(quantities[key])})
			}
			for _, o := range ops {
				if o.Status != "posted" || o.OccurredAt.After(at) {
					continue
				}
				if o.Timing != nil && (o.Timing.OccurredDate != "" || o.Timing.SettlementDate != "") && !slices.Contains(s.Issues, "date_only_timing") {
					s.Issues = append(s.Issues, "date_only_timing")
				}
				if o.TradeFinancials != nil && strings.TrimSpace(o.TradeFinancials.DifferenceReason) != "" && !slices.Contains(s.Issues, "trade_settlement_difference_reviewed") {
					s.Issues = append(s.Issues, "trade_settlement_difference_reviewed")
				}
			}
		}
		out = append(out, s)
	}
	return out, true, nil
}
