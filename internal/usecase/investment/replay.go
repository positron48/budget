package investment

import (
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/usecase/investmentcalc"
)

// ReplayOperationError identifies a failing operation without changing the
// public error message or domain error classification.
type ReplayOperationError struct {
	OperationID   string
	InstrumentKey string
	Cause         error
}

func (e *ReplayOperationError) Error() string { return e.Cause.Error() }
func (e *ReplayOperationError) Unwrap() error { return e.Cause }

// ReplayCashError reports the whole atomic timestamp, not an arbitrary leg.
type ReplayCashError struct {
	At           time.Time
	Balance      d.InvestmentCash
	OperationIDs []string
	Cause        error
}

func (e *ReplayCashError) Error() string { return e.Cause.Error() }
func (e *ReplayCashError) Unwrap() error { return e.Cause }

type PositionBookValues struct{ Quantity, CleanCost, AcquiredAccrued *big.Rat }
type RemainingLot struct {
	LotID, OriginLedgerID                string
	OriginSequence                       int64
	OriginID                             string
	AcquiredAt                           time.Time
	Quantity, CleanCost, AcquiredAccrued *big.Rat
}
type LotChange struct {
	LedgerID, LedgerName             string
	Sequence                         int64
	OperationID, Kind, InstrumentKey string
	At                               time.Time
	Before, After                    *RemainingLot
}
type ReplayResult struct {
	SecurityTransfers map[string]SecurityTransferReceipt
	LotHistory        []LotChange
	ExactLots         map[string][]RemainingLot
	ExactPositions    map[string]PositionBookValues
	ProfitEvents      []investmentcalc.ProfitEvent
	Snapshot          d.InvestmentSnapshot
	CashLegs          []d.InvestmentCashLeg
	SecurityLegs      []d.InvestmentSecurityLeg
}
type position struct {
	lotOrigins                                                      map[string]lotOrigin
	cleanUnknown, accruedUnknown                                    map[string]bool
	lotDates                                                        map[string]time.Time
	realizedUnknown, realizedAccruedUnknown, unknownAcquisitionDate bool
	couponGross, couponTax, couponRecovered                         big.Rat
	couponUnknown                                                   bool
	nominal                                                         string
	currency                                                        string
	clean, accrued                                                  investmentcalc.Book
	realized, realizedAccrued                                       *big.Rat
}

func decimalString(n *big.Rat) string {
	v := n.FloatString(12)
	v = strings.TrimRight(strings.TrimRight(v, "0"), ".")
	if v == "-0" {
		return "0"
	}
	return v
}
func signed(v string, negative bool) string {
	if v == "0" {
		return v
	}
	if negative {
		return "-" + v
	}
	return v
}
func inconsistent(message string) error {
	return fmt.Errorf("%w: %s", d.ErrInvestmentPrecondition, message)
}

// Replay is deterministic over immutable confirmed facts. It validates the
// entire history (including future settlements) before exposing an as-of view.
// A rejected backdated operation cannot silently invalidate later trades.
func Replay(ledger d.InvestmentLedger, input []d.InvestmentOperation, asOf time.Time) (ReplayResult, error) {
	return replay(ledger, input, asOf, replayOptions{})
}

// ReplayLotHistory traces only requested origins. The ordinary replay does not
// allocate history. Future facts are validated but never included in the trace.
func ReplayLotHistory(ledger d.InvestmentLedger, input []d.InvestmentOperation, asOf time.Time, origins []string) (ReplayResult, error) {
	selected := map[string]bool{}
	for _, id := range origins {
		selected[id] = true
	}
	return replay(ledger, input, asOf, replayOptions{origins: selected})
}
func replay(ledger d.InvestmentLedger, input []d.InvestmentOperation, asOf time.Time, options replayOptions) (ReplayResult, error) {
	origins := options.origins
	result := ReplayResult{SecurityTransfers: map[string]SecurityTransferReceipt{}, ExactLots: map[string][]RemainingLot{}, ExactPositions: map[string]PositionBookValues{}, Snapshot: d.InvestmentSnapshot{Ledger: ledger, AsOf: asOf, Cash: []d.InvestmentCash{}, Positions: []d.InvestmentPosition{}, ValuationStatus: "market_data_required"}}
	if asOf.IsZero() || ledger.StartedAt.IsZero() {
		return result, invalid("ledger start and cutoff required")
	}
	if asOf.Before(ledger.StartedAt) {
		return result, inconsistent("requested cutoff precedes ledger opening")
	}
	if len(input) > MaxOperations {
		return result, d.ErrInvestmentLimit
	}
	ops := slices.Clone(input)
	for i := range ops {
		if ops[i].Status == "posted" && ops[i].Timing != nil {
			if err := ValidateOperation(&ops[i]); err != nil {
				return result, err
			}
		}
	}
	slices.SortFunc(ops, func(a, b d.InvestmentOperation) int {
		if c := a.OccurredAt.Compare(b.OccurredAt); c != 0 {
			return c
		}
		if aPriority, bPriority := operationTimingPriority(a), operationTimingPriority(b); aPriority != bPriority {
			if aPriority < bPriority {
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
	books := map[string]*position{}
	atCutoff := map[string]d.InvestmentPosition{}
	openingCurrencies := map[string]bool{}
	acquisitionOrder := map[string]int{}
	distributionEvents := map[string]bool{}
	reconciliations := map[string]d.InvestmentOperation{}
	deficitReviewed := map[string]bool{}
	ids := map[string]bool{}
	sequences := map[int64]bool{}
	addCash := func(o d.InvestmentOperation, bucket, component, amount string, at time.Time) {
		if amount != "0" {
			result.CashLegs = append(result.CashLegs, d.InvestmentCashLeg{OperationID: o.ID, Currency: o.Currency, Bucket: bucket, Component: component, Amount: amount, EffectiveAt: at})
		}
	}
	var currentOperation d.InvestmentOperation
	traceLimit := false
	capturePosition := func(key string, p *position) d.InvestmentPosition {
		value := positionSnapshot(key, p)
		quantity, cost := p.clean.Totals()
		_, accrued := p.accrued.Totals()
		if value.CleanCost == "" {
			cost = nil
		}
		if value.AcquiredAccrued == "" {
			accrued = nil
		}
		result.ExactPositions[key] = PositionBookValues{Quantity: quantity, CleanCost: cost, AcquiredAccrued: accrued}
		accruedLots := map[string]*big.Rat{}
		for _, lot := range p.accrued.Lots() {
			accruedLots[lot.OriginID] = lot.Cost
		}
		lots := []RemainingLot{}
		for _, lot := range p.clean.Lots() {
			clean, interest := lot.Cost, accruedLots[lot.OriginID]
			if p.cleanUnknown[lot.OriginID] {
				clean = nil
			}
			if p.accruedUnknown[lot.OriginID] {
				interest = nil
			}
			origin := p.lotOrigins[lot.OriginID]
			if origin.ID == "" {
				origin = lotOrigin{ID: lot.OriginID, LedgerID: ledger.ID, Sequence: options.sequences[lot.OriginID]}
			}
			lots = append(lots, RemainingLot{LotID: lot.OriginID, OriginID: origin.ID, OriginLedgerID: origin.LedgerID, OriginSequence: origin.Sequence, AcquiredAt: p.lotDates[lot.OriginID], Quantity: lot.Quantity, CleanCost: clean, AcquiredAccrued: interest})
		}
		if len(origins) > 0 {
			before := map[string]*RemainingLot{}
			after := map[string]*RemainingLot{}
			order := []string{}
			for i := range result.ExactLots[key] {
				lot := &result.ExactLots[key][i]
				if origins[lot.OriginID] {
					before[lot.LotID] = lot
					order = append(order, lot.LotID)
				}
			}
			for i := range lots {
				lot := &lots[i]
				if origins[lot.OriginID] {
					after[lot.LotID] = lot
					if before[lot.LotID] == nil {
						order = append(order, lot.LotID)
					}
				}
			}
			for _, id := range order {
				if !sameRemainingLot(before[id], after[id]) {
					if len(result.LotHistory) >= 10000 {
						traceLimit = true
						break
					}
					result.LotHistory = append(result.LotHistory, LotChange{LedgerID: ledger.ID, LedgerName: ledger.Name, Sequence: currentOperation.Sequence, OperationID: currentOperation.ID, Kind: currentOperation.Kind, InstrumentKey: key, At: currentOperation.OccurredAt, Before: cloneRemainingLot(before[id]), After: cloneRemainingLot(after[id])})
				}
			}
		}
		result.ExactLots[key] = lots
		return value
	}
	profitEvent := func(o d.InvestmentOperation, kind string, amount *big.Rat, reason string) {
		if o.OccurredAt.After(asOf) {
			return
		}
		if amount != nil {
			amount = new(big.Rat).Set(amount)
		}
		result.ProfitEvents = append(result.ProfitEvents, investmentcalc.ProfitEvent{OperationID: o.ID, LedgerID: o.LedgerID, InstrumentKey: o.InstrumentKey, Currency: o.Currency, Kind: kind, Amount: amount, Reason: reason, At: o.OccurredAt})
	}
	for _, o := range ops {
		currentOperation = o
		if o.Status == "voided" {
			continue
		}
		if o.Status != "posted" || o.ID == "" || ids[o.ID] || o.Sequence < 1 || sequences[o.Sequence] {
			return result, invalid("unique operation identity and sequence required")
		}
		acquisitionOrder[o.ID] = len(ids)
		if options.order != nil {
			acquisitionOrder[o.ID] = options.order[o.ID]
		}
		ids[o.ID] = true
		sequences[o.Sequence] = true
		if o.LedgerID != ledger.ID || o.TenantID != ledger.TenantID {
			return result, invalid("operation scope mismatch")
		}
		if err := ValidateOperation(&o); err != nil {
			return result, err
		}
		if o.ImportedCashDeficitReason != "" {
			if err := validateCashDeficitSource(o); err != nil {
				return result, err
			}
			deficitReviewed[o.ID] = true
			if !o.OccurredAt.After(asOf) {
				addSnapshotIssue(&result.Snapshot, "imported_cash_deficit_reviewed")
			}
		}
		currentOperation = o
		if o.Timing != nil && (o.Timing.OccurredDate != "" || o.Timing.SettlementDate != "") && !o.OccurredAt.After(asOf) && !slices.Contains(result.Snapshot.Issues, "date_only_timing") {
			result.Snapshot.Issues = append(result.Snapshot.Issues, "date_only_timing")
		}
		if o.TradeFinancials != nil && strings.TrimSpace(o.TradeFinancials.DifferenceReason) != "" && !o.OccurredAt.After(asOf) && !slices.Contains(result.Snapshot.Issues, "trade_settlement_difference_reviewed") {
			result.Snapshot.Issues = append(result.Snapshot.Issues, "trade_settlement_difference_reviewed")
		}
		if o.TaxReviewRequired && !o.OccurredAt.After(asOf) && !slices.Contains(result.Snapshot.Issues, "tax_review_required") {
			result.Snapshot.Issues = append(result.Snapshot.Issues, "tax_review_required")
		}
		if o.OccurredAt.Before(ledger.StartedAt) {
			return result, inconsistent("operation precedes ledger opening")
		}
		if o.Kind == "opening_cash" || o.Kind == "opening_position" {
			if !o.OccurredAt.Equal(ledger.StartedAt) {
				return result, inconsistent("opening must match ledger start")
			}
			if o.Kind == "opening_cash" {
				if openingCurrencies[o.Currency] {
					return result, inconsistent("only one opening cash balance per currency")
				}
				openingCurrencies[o.Currency] = true
			}
			if !o.OccurredAt.After(asOf) && !slices.Contains(result.Snapshot.Issues, "history_before_opening_unknown") {
				result.Snapshot.Issues = append(result.Snapshot.Issues, "history_before_opening_unknown")
			}
		}
		if o.SettlementAssumed && !o.OccurredAt.After(asOf) && !slices.Contains(result.Snapshot.Issues, "settlement_assumed_trade_time") {
			result.Snapshot.Issues = append(result.Snapshot.Issues, "settlement_assumed_trade_time")
		}
		switch o.Kind {
		case "security_transfer_out", "security_transfer_in":
			if options.receipt == nil {
				return result, inconsistent("security transfer requires linked portfolio replay")
			}
			p := books[o.InstrumentKey]
			if o.Kind == "security_transfer_out" {
				receipt, err := extractSecurityTransfer(p, o, ledger, options.sequences)
				if err != nil {
					return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: err}
				}
				result.SecurityTransfers[o.TransferID] = receipt
			} else {
				receipt, err := options.receipt(o.TransferID)
				if err != nil {
					return result, err
				}
				p, err = receiveSecurityTransfer(p, o, receipt, acquisitionOrder, options.order)
				if err != nil {
					return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: err}
				}
				books[o.InstrumentKey] = p
			}
			result.SecurityLegs = append(result.SecurityLegs, d.InvestmentSecurityLeg{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Quantity: signed(o.Quantity, o.Kind == "security_transfer_out"), EffectiveAt: o.OccurredAt})
			if !o.OccurredAt.After(asOf) {
				v := capturePosition(o.InstrumentKey, p)
				atCutoff[o.InstrumentKey] = v
				for _, issue := range []struct {
					present bool
					code    string
				}{{v.CleanCost == "", "unknown_acquisition_cost"}, {v.AcquiredAccrued == "", "unknown_acquired_interest"}, {p.unknownAcquisitionDate, "historical_acquisition_date_unknown"}} {
					if issue.present && !slices.Contains(result.Snapshot.Issues, issue.code) {
						result.Snapshot.Issues = append(result.Snapshot.Issues, issue.code)
					}
				}
			}
			if options.stop == o.ID {
				return result, nil
			}
		case "distribution":
			if distributionEvents[o.Distribution.EventKey] {
				return result, inconsistent("corporate event already applied")
			}
			distributionEvents[o.Distribution.EventKey] = true
			legs, err := applyDistribution(books, o, acquisitionOrder)
			if err != nil {
				return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: err}
			}
			result.SecurityLegs = append(result.SecurityLegs, legs...)
			if !o.OccurredAt.After(asOf) {
				for _, key := range []string{o.InstrumentKey, o.Distribution.TargetInstrumentKey} {
					value := capturePosition(key, books[key])
					atCutoff[key] = value
					if value.CleanCost == "" && !slices.Contains(result.Snapshot.Issues, "unknown_acquisition_cost") {
						result.Snapshot.Issues = append(result.Snapshot.Issues, "unknown_acquisition_cost")
					}
				}
			}
		case "buy", "sell", "split", "amortization", "redemption", "coupon", "opening_position":
			p := books[o.InstrumentKey]
			if p == nil {
				p = &position{cleanUnknown: map[string]bool{}, accruedUnknown: map[string]bool{}, lotDates: map[string]time.Time{}, currency: o.Currency, realized: new(big.Rat), realizedAccrued: new(big.Rat)}
				books[o.InstrumentKey] = p
			}
			if o.Kind != "split" && p.currency != o.Currency {
				return result, inconsistent("instrument cost currency changed; explicit conversion required")
			}
			if o.Kind == "amortization" || o.Kind == "redemption" {
				if p.nominal != "" && p.nominal != o.NominalBefore {
					return result, inconsistent("prior nominal does not match preceding corporate event")
				}
				if o.Kind == "redemption" {
					q, _ := p.clean.Totals()
					if q.Cmp(rational(o.Quantity)) != 0 {
						return result, inconsistent("full redemption must close the entire position")
					}
				}
				p.nominal = o.NominalAfter
			}
			if o.Kind == "buy" && p.nominal == "0" {
				return result, inconsistent("cannot buy a redeemed instrument")
			}
			switch o.Kind {
			case "buy", "opening_position":
				if p.lotOrigins == nil {
					p.lotOrigins = map[string]lotOrigin{}
				}
				p.lotOrigins[o.ID] = lotOrigin{ID: o.ID, LedgerID: ledger.ID, Sequence: o.Sequence}
				p.lotDates[o.ID] = o.OccurredAt
				if o.Kind == "opening_position" {
					p.cleanUnknown[o.ID] = !o.OpeningCostKnown
					p.accruedUnknown[o.ID] = !o.OpeningAccruedKnown
					p.lotDates[o.ID] = o.AcquiredAt
					if o.AcquiredAt.IsZero() {
						p.unknownAcquisitionDate = true
					}
				}

				principal, accrued, fee := tradeBookValues(o)
				if err := p.clean.BuyCostExact(o.ID, o.Quantity, new(big.Rat).Add(principal, fee)); err != nil {
					return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: inconsistent(err.Error())}
				}
				if err := p.accrued.BuyCostExact(o.ID, o.Quantity, accrued); err != nil {
					return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: inconsistent(err.Error())}
				}
			case "sell", "redemption":
				principal, accrued, fee := tradeBookValues(o)
				cleanProceeds := new(big.Rat).Sub(principal, fee)
				matches, gain, err := p.clean.SellProceedsExact(o.Quantity, cleanProceeds)
				if err != nil {
					return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: inconsistent(err.Error())}
				}
				cleanKnown, accruedKnown := true, true
				for _, m := range matches {
					if p.cleanUnknown[m.OriginID] {
						p.realizedUnknown = true
						cleanKnown = false
					}
					if p.accruedUnknown[m.OriginID] {
						p.realizedAccruedUnknown = true
						accruedKnown = false
					}
				}
				p.realized.Add(p.realized, gain)
				if cleanKnown {
					profitEvent(o, "realized_clean", gain, "")
				} else {
					profitEvent(o, "realized_clean", nil, "unknown_acquisition_cost")
				}
				_, gain, err = p.accrued.SellProceedsExact(o.Quantity, accrued)
				if err != nil {
					return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: inconsistent(err.Error())}
				}
				p.realizedAccrued.Add(p.realizedAccrued, gain)
				if accruedKnown {
					profitEvent(o, "realized_accrued", gain, "")
				} else {
					profitEvent(o, "realized_accrued", nil, "unknown_acquired_interest")
				}
			case "coupon":
				var earned *big.Rat
				if !o.CouponAllocationConfirmed {
					p.couponUnknown = true
				} else {
					allocations := map[string]string{}
					for _, a := range o.AccruedRecoveries {
						if p.accruedUnknown[a.OriginID] {
							return result, inconsistent("confirm opening accrued cost before recovery")
						}
						allocations[a.OriginID] = a.Amount
						if a.AllRemaining {
							allocations[a.OriginID] = "all"
						}
					}
					recovered, err := p.accrued.RecoverCosts(allocations)
					if err != nil {
						return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: inconsistent(err.Error())}
					}
					if recovered.Cmp(rational(o.Amount)) > 0 {
						return result, inconsistent("recovered accrued interest exceeds gross coupon")
					}
					p.couponRecovered.Add(&p.couponRecovered, recovered)
					earned = new(big.Rat).Sub(rational(o.Amount), recovered)
				}
				if earned == nil {
					profitEvent(o, "earned_income", nil, "coupon_allocation_required")
				} else {
					profitEvent(o, "earned_income", earned, "")
				}
				p.couponGross.Add(&p.couponGross, rational(o.Amount))
				p.couponTax.Add(&p.couponTax, rational(o.Tax))
				addCash(o, "settled", "income", o.Amount, o.OccurredAt)
				addCash(o, "settled", "tax", signed(o.Tax, true), o.OccurredAt)
			case "amortization":
				known := true
				for _, lot := range p.clean.Lots() {
					if p.cleanUnknown[lot.OriginID] {
						p.realizedUnknown = true
						known = false
					}
				}
				gain, err := p.clean.Amortize(o.NominalBefore, o.NominalAfter, o.Amount)
				if err != nil {
					return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: inconsistent(err.Error())}
				}
				p.realized.Add(p.realized, gain)
				if known {
					profitEvent(o, "realized_clean", gain, "")
				} else {
					profitEvent(o, "realized_clean", nil, "unknown_acquisition_cost")
				}
			case "split":
				if p.nominal != "" {
					return result, inconsistent("bond denomination change requires explicit nominal conversion")
				}
				before, _ := p.clean.Totals()
				if before.Sign() == 0 {
					return result, inconsistent("split requires an existing position")
				}
				if err := p.clean.Split(o.SplitNew, o.SplitOld); err != nil {
					return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: inconsistent(err.Error())}
				}
				if err := p.accrued.Split(o.SplitNew, o.SplitOld); err != nil {
					return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: inconsistent(err.Error())}
				}
				after, _ := p.clean.Totals()
				// Fractional/cash-in-lieu actions need explicit quantities; do not round.
				normalized, err := normalize(after.FloatString(10), 28, 10)
				if err != nil || rational(normalized).Cmp(after) != 0 {
					return result, inconsistent("split quantity requires explicit fractional-share resolution")
				}
				result.SecurityLegs = append(result.SecurityLegs, d.InvestmentSecurityLeg{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Quantity: decimalString(new(big.Rat).Sub(after, before)), EffectiveAt: o.OccurredAt})
			}
			if o.Kind == "opening_position" {
				result.SecurityLegs = append(result.SecurityLegs, d.InvestmentSecurityLeg{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Quantity: o.Quantity, EffectiveAt: o.OccurredAt})
			}
			if o.Kind != "split" && o.Kind != "coupon" && o.Kind != "opening_position" {
				sell := o.Kind == "sell" || o.Kind == "redemption" || o.Kind == "amortization"
				if o.Kind != "amortization" {
					result.SecurityLegs = append(result.SecurityLegs, d.InvestmentSecurityLeg{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Quantity: signed(o.Quantity, sell), EffectiveAt: o.OccurredAt})
				}
				bucket := "payable"
				if sell {
					bucket = "receivable"
				}
				if o.TradeFinancials != nil {
					for _, part := range tradeCashParts(o) {
						amount := rational(part.amount)
						if amount.Sign() == 0 {
							continue
						}
						bucket := "receivable"
						if amount.Sign() < 0 {
							bucket = "payable"
						}
						native := o
						native.Currency = part.currency
						pending := decimalString(new(big.Rat).Abs(amount))
						addCash(native, bucket, part.component, pending, o.OccurredAt)
						addCash(native, bucket, part.component, signed(pending, true), part.at)
						addCash(native, "settled", part.component, part.amount, part.at)
					}
				} else {
					for _, part := range []struct{ component, amount string }{{"principal", o.Amount}, {"accrued", o.Accrued}, {"fee", o.Fee}} {
						amount := part.amount
						if sell && part.component == "fee" {
							amount = signed(amount, true)
						}
						addCash(o, bucket, part.component, amount, o.OccurredAt)
						addCash(o, bucket, part.component, decimalString(new(big.Rat).Neg(rational(amount))), o.SettlementAt)
						cash := rational(amount)
						if !sell {
							cash.Neg(cash)
						}
						addCash(o, "settled", part.component, decimalString(cash), o.SettlementAt)
					}
				}
			}
			if !o.OccurredAt.After(asOf) {
				v := capturePosition(o.InstrumentKey, p)
				for _, issue := range []struct {
					present bool
					code    string
				}{{v.CleanCost == "" || v.RealizedClean == "", "unknown_acquisition_cost"}, {v.AcquiredAccrued == "" || v.RealizedAccrued == "", "unknown_acquired_interest"}, {p.unknownAcquisitionDate, "historical_acquisition_date_unknown"}} {
					if issue.present && !slices.Contains(result.Snapshot.Issues, issue.code) {
						result.Snapshot.Issues = append(result.Snapshot.Issues, issue.code)
					}
				}
				if p.couponUnknown && !slices.Contains(result.Snapshot.Issues, "coupon_allocation_required") {
					result.Snapshot.Issues = append(result.Snapshot.Issues, "coupon_allocation_required")
				}
				atCutoff[o.InstrumentKey] = v
			}
		case "fx":
			// Both sides are internal exchange legs, never external funding flows.
			for _, part := range []struct{ currency, bucket, component, amount string }{
				{o.Currency, "payable", "fx_out", o.Amount},
				{o.Currency, "payable", "fee", o.Fee},
				{o.ToCurrency, "receivable", "fx_in", o.ToAmount},
			} {
				legOperation := o
				legOperation.Currency = part.currency
				addCash(legOperation, part.bucket, part.component, part.amount, o.OccurredAt)
				addCash(legOperation, part.bucket, part.component, signed(part.amount, true), o.SettlementAt)
				addCash(legOperation, "settled", part.component, signed(part.amount, part.bucket == "payable"), o.SettlementAt)
			}
		case "cash_reconciliation":
			terms := o.CashReconciliation
			delta := decimalString(new(big.Rat).Sub(rational(terms.ObservedSettled), rational(terms.ExpectedSettled)))
			// Include a zero marker so unchanged cash still has a checked, auditable fact.
			result.CashLegs = append(result.CashLegs, d.InvestmentCashLeg{OperationID: o.ID, Currency: o.Currency, Bucket: "settled", Component: o.Kind, Amount: delta, EffectiveAt: o.OccurredAt})
			reconciliations[o.ID] = o
			if delta != "0" && !o.OccurredAt.After(asOf) && !slices.Contains(result.Snapshot.Issues, "cash_reconciliation_unclassified") {
				result.Snapshot.Issues = append(result.Snapshot.Issues, "cash_reconciliation_unclassified")
			}
		case "deposit", "withdrawal", "fee", "tax", "tax_refund", "transfer_in", "transfer_out", "opening_cash", "cash_interest":
			addCash(o, "settled", o.Kind, signed(o.Amount, o.Kind != "deposit" && o.Kind != "transfer_in" && o.Kind != "opening_cash" && o.Kind != "cash_interest" && o.Kind != "tax_refund"), o.OccurredAt)
		case "dividend", "repo_income":
			addCash(o, "settled", "income", o.Amount, o.OccurredAt)
			addCash(o, "settled", "tax", signed(o.Tax, true), o.OccurredAt)
		}
		switch o.Kind {
		case "dividend", "repo_income", "cash_interest":
			profitEvent(o, "earned_income", rational(o.Amount), "")
		case "fee":
			profitEvent(o, "fees", new(big.Rat).Neg(rational(o.Amount)), "")
		case "tax_refund":
			profitEvent(o, "taxes", rational(o.Amount), "")
		case "tax":
			profitEvent(o, "taxes", new(big.Rat).Neg(rational(o.Amount)), "")
		}
		if o.TradeFinancials != nil {
			native := o
			native.Currency = o.TradeFinancials.Tax.Currency
			native.OccurredAt = o.TradeFinancials.Tax.At
			if o.TradeFinancials.Tax.Amount != "0" {
				profitEvent(native, "taxes", new(big.Rat).Neg(rational(o.TradeFinancials.Tax.Amount)), "")
			}
			native.Currency = o.TradeFinancials.Fee.Currency
			native.OccurredAt = o.TradeFinancials.Fee.At
			if o.TradeFinancials.Fee.Amount != "0" {
				profitEvent(native, "trade_fees_in_cost", rational(o.TradeFinancials.Fee.Amount), "")
			}
		} else if rational(o.Tax).Sign() != 0 {
			profitEvent(o, "taxes", new(big.Rat).Neg(rational(o.Tax)), "")
		}
		if o.TradeFinancials == nil && rational(o.Fee).Sign() != 0 {
			switch o.Kind {
			case "buy", "sell", "redemption":
				profitEvent(o, "trade_fees_in_cost", rational(o.Fee), "")
			case "fx":
				profitEvent(o, "fees", new(big.Rat).Neg(rational(o.Fee)), "")
			}
		}

	}
	if traceLimit {
		return result, d.ErrInvestmentLimit
	}
	// Apply every timestamp atomically: trade and same-time settlement legs must
	// not create transient negative cash due to the ordering of their components.
	slices.SortStableFunc(result.CashLegs, func(a, b d.InvestmentCashLeg) int { return a.EffectiveAt.Compare(b.EffectiveAt) })
	cash := map[string]map[string]*big.Rat{}
	visible := map[string]d.InvestmentCash{}
	for i := 0; i < len(result.CashLegs); {
		at := result.CashLegs[i].EffectiveAt
		j := i
		priorDeficits := cashDeficits(cash)
		for j < len(result.CashLegs) && result.CashLegs[j].EffectiveAt.Equal(at) {
			leg := result.CashLegs[j]
			// Each materialized money leg must fit its database decimal contract.
			if _, err := normalize(strings.TrimPrefix(leg.Amount, "-"), 36, 12); err != nil {
				return result, err
			}
			if cash[leg.Currency] == nil {
				cash[leg.Currency] = map[string]*big.Rat{"settled": new(big.Rat), "receivable": new(big.Rat), "payable": new(big.Rat)}
			}
			cash[leg.Currency][leg.Bucket].Add(cash[leg.Currency][leg.Bucket], rational(leg.Amount))
			j++
		}
		// All ordinary legs at a timestamp apply atomically before its reconciliation.
		// Two observations of one currency at the same timestamp are ambiguous.
		checked := map[string]bool{}
		for _, leg := range result.CashLegs[i:j] {
			o, ok := reconciliations[leg.OperationID]
			if !ok {
				continue
			}
			terms := o.CashReconciliation
			prior := new(big.Rat).Sub(cash[o.Currency]["settled"], rational(leg.Amount))
			if checked[o.Currency] || prior.Cmp(rational(terms.ExpectedSettled)) != 0 {
				return result, &ReplayOperationError{OperationID: o.ID, Cause: inconsistent("cash reconciliation baseline changed; review the observation")}
			}
			checked[o.Currency] = true
		}
		currencies := make([]string, 0, len(cash))
		for c := range cash {
			currencies = append(currencies, c)
		}
		slices.Sort(currencies)
		for _, c := range currencies {
			v := cash[c]
			economic := new(big.Rat).Sub(new(big.Rat).Add(v["settled"], v["receivable"]), v["payable"])
			allowance := cashDeficitAllowance(result.CashLegs[i:j], c, deficitReviewed)
			prior := priorDeficits[c]
			if deficitExceeds(v["settled"], prior.settled, allowance.settled) || deficitExceeds(economic, prior.economic, allowance.economic) || v["receivable"].Sign() < 0 || v["payable"].Sign() < 0 {
				ids := []string{}
				for _, leg := range result.CashLegs[i:j] {
					if leg.Currency == c && !slices.Contains(ids, leg.OperationID) {
						ids = append(ids, leg.OperationID)
					}
				}
				slices.Sort(ids)
				return result, &ReplayCashError{At: at, Balance: d.InvestmentCash{Currency: c, Settled: decimalString(v["settled"]), Receivable: decimalString(v["receivable"]), Payable: decimalString(v["payable"]), Economic: decimalString(economic)}, OperationIDs: ids, Cause: inconsistent("negative cash; confirm opening balance or missing funding")}
			}
			if !at.After(asOf) {
				if economic.Sign() < 0 || v["settled"].Sign() < 0 {
					addSnapshotIssue(&result.Snapshot, "imported_cash_history_incomplete")
				}
				visible[c] = d.InvestmentCash{Currency: c, Settled: decimalString(v["settled"]), Receivable: decimalString(v["receivable"]), Payable: decimalString(v["payable"]), Economic: decimalString(economic)}
			}
		}
		i = j
	}
	for _, v := range visible {
		if rational(v.Settled).Sign() < 0 || rational(v.Economic).Sign() < 0 {
			addSnapshotIssue(&result.Snapshot, "negative_imported_cash")
		}
		result.Snapshot.Cash = append(result.Snapshot.Cash, v)
	}
	for _, v := range atCutoff {
		result.Snapshot.Positions = append(result.Snapshot.Positions, v)
	}
	slices.SortFunc(result.Snapshot.Cash, func(a, b d.InvestmentCash) int { return strings.Compare(a.Currency, b.Currency) })
	slices.SortFunc(result.Snapshot.Positions, func(a, b d.InvestmentPosition) int { return strings.Compare(a.InstrumentKey, b.InstrumentKey) })
	return result, nil
}

func positionSnapshot(instrument string, p *position) d.InvestmentPosition {
	q, c := p.clean.Totals()
	_, accrued := p.accrued.Totals()
	v := d.InvestmentPosition{OutstandingNominal: p.nominal, InstrumentKey: instrument, Currency: p.currency, Quantity: decimalString(q), CleanCost: decimalString(c), AcquiredAccrued: decimalString(accrued), RealizedClean: decimalString(p.realized), RealizedAccrued: decimalString(p.realizedAccrued), CouponGross: decimalString(&p.couponGross), CouponTax: decimalString(&p.couponTax), CouponRecovered: decimalString(&p.couponRecovered)}
	if !p.couponUnknown {
		v.CouponEarned = decimalString(new(big.Rat).Sub(&p.couponGross, &p.couponRecovered))
	}
	accruedLots := map[string]string{}
	for _, lot := range p.accrued.Lots() {
		accruedLots[lot.OriginID] = decimalString(lot.Cost)
	}
	for _, lot := range p.clean.Lots() {
		item := d.InvestmentLot{OriginID: lot.OriginID, Quantity: decimalString(lot.Quantity), CleanCost: decimalString(lot.Cost), AcquiredAccrued: accruedLots[lot.OriginID], AcquiredAt: p.lotDates[lot.OriginID]}
		if origin := p.lotOrigins[lot.OriginID]; origin.ID != "" && origin.ID != lot.OriginID {
			item.AcquisitionOperationID = origin.ID
			item.OriginLedgerID = origin.LedgerID
			item.OriginSequence = origin.Sequence
		}
		if p.cleanUnknown[lot.OriginID] {
			item.CleanCost = ""
			v.CleanCost = ""
		}
		if p.accruedUnknown[lot.OriginID] {
			item.AcquiredAccrued = ""
			v.AcquiredAccrued = ""
		}
		v.Lots = append(v.Lots, item)
	}
	if p.realizedUnknown {
		v.RealizedClean = ""
	}
	if p.realizedAccruedUnknown {
		v.RealizedAccrued = ""
	}
	return v
}

func sameRemainingLot(a, b *RemainingLot) bool {
	if a == nil || b == nil {
		return a == b
	}
	same := func(x, y *big.Rat) bool {
		if x == nil || y == nil {
			return x == y
		}
		return x.Cmp(y) == 0
	}
	return a.LotID == b.LotID && a.OriginID == b.OriginID && a.OriginLedgerID == b.OriginLedgerID && a.OriginSequence == b.OriginSequence && a.AcquiredAt.Equal(b.AcquiredAt) && same(a.Quantity, b.Quantity) && same(a.CleanCost, b.CleanCost) && same(a.AcquiredAccrued, b.AcquiredAccrued)
}

func cloneRemainingLot(v *RemainingLot) *RemainingLot {
	if v == nil {
		return nil
	}
	out := *v
	clone := func(r *big.Rat) *big.Rat {
		if r == nil {
			return nil
		}
		return new(big.Rat).Set(r)
	}
	out.Quantity = clone(v.Quantity)
	out.CleanCost = clone(v.CleanCost)
	out.AcquiredAccrued = clone(v.AcquiredAccrued)
	return &out
}
