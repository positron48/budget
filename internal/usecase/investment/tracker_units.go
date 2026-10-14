package investment

import (
	"math/big"
	"slices"
	"strings"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

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
	if len(input) > MaxOperations || len(cutoffs) > 31 {
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
	appendSnapshot := func(at time.Time) {
		s := d.InvestmentSnapshot{Ledger: ledger, AsOf: at, Positions: []d.InvestmentPosition{}}
		if !at.Before(ledger.StartedAt) {
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
