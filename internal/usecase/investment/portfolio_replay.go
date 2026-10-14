package investment

import (
	"math/big"
	"slices"
	"strings"
	"time"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

// Basic portfolios own security units and native acquisition lots. Legacy cash
// books and paired corporate transfers retain their existing replay contract.
func nativePortfolioHistory(ops []d.InvestmentOperation) bool {
	for _, o := range ops {
		if o.Status == "voided" {
			continue
		}
		if o.CouponAllocationConfirmed || len(o.AccruedRecoveries) > 0 || o.BudgetTransferID != "" {
			return false
		}
		switch o.Kind {
		case "buy", "sell", "opening_position", "split", "redemption", "dividend", "coupon", "repo_income", "cash_interest", "fee", "tax", "tax_refund", "deposit", "withdrawal", "opening_cash", "cash_reconciliation":
		default:
			return false
		}
	}
	return true
}

type nativePortfolioBook struct {
	currency, nominal                  string
	lots                               []RemainingLot
	realized, realizedAccrued, coupons map[string]*big.Rat
	realizedUnknown, accruedUnknown    bool
}

func nativeAdd(values map[string]*big.Rat, currency string, amount *big.Rat) {
	if values[currency] == nil {
		values[currency] = new(big.Rat)
	}
	values[currency].Add(values[currency], amount)
}
func nativeCloneBooks(books map[string]*nativePortfolioBook) map[string]*nativePortfolioBook {
	out := map[string]*nativePortfolioBook{}
	for key, b := range books {
		copy := *b
		copy.lots = make([]RemainingLot, len(b.lots))
		for i := range b.lots {
			copy.lots[i] = *cloneRemainingLot(&b.lots[i])
		}
		clone := func(values map[string]*big.Rat) map[string]*big.Rat {
			result := map[string]*big.Rat{}
			for c, v := range values {
				result[c] = new(big.Rat).Set(v)
			}
			return result
		}
		copy.realized, copy.realizedAccrued, copy.coupons = clone(b.realized), clone(b.realizedAccrued), clone(b.coupons)
		out[key] = &copy
	}
	return out
}
func nativeQuantity(b *nativePortfolioBook) *big.Rat {
	q := new(big.Rat)
	for _, lot := range b.lots {
		q.Add(q, lot.Quantity)
	}
	return q
}
func nativeScalar(values map[string]*big.Rat, currency string, unknown bool) string {
	if unknown {
		return ""
	}
	total := new(big.Rat)
	for c, v := range values {
		if c != currency && v.Sign() != 0 {
			return ""
		}
		total.Add(total, v)
	}
	return decimalString(total)
}

func replayNativePortfolio(ledger d.InvestmentLedger, input []d.InvestmentOperation, asOf time.Time, options replayOptions) (ReplayResult, error) {
	result := ReplayResult{SecurityTransfers: map[string]SecurityTransferReceipt{}, ExactLots: map[string][]RemainingLot{}, ExactPositions: map[string]PositionBookValues{}, Snapshot: d.InvestmentSnapshot{Ledger: ledger, AsOf: asOf, Cash: []d.InvestmentCash{}, Positions: []d.InvestmentPosition{}, ValuationStatus: "market_data_required"}}
	if asOf.IsZero() || ledger.StartedAt.IsZero() {
		return result, invalid("ledger start and cutoff required")
	}
	if len(input) > MaxOperations {
		return result, d.ErrInvestmentLimit
	}
	ops := slices.Clone(input)
	for i := range ops {
		if ops[i].Status != "voided" {
			if e := ValidateOperation(&ops[i]); e != nil {
				return result, e
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
	ids, sequences := map[string]bool{}, map[int64]bool{}
	books := map[string]*nativePortfolioBook{}
	var cutoff map[string]*nativePortfolioBook
	event := func(o d.InvestmentOperation, currency, kind string, amount *big.Rat, reason string) {
		if o.OccurredAt.After(asOf) {
			return
		}
		var value *big.Rat
		if amount != nil {
			value = new(big.Rat).Set(amount)
		}
		result.ProfitEvents = append(result.ProfitEvents, calc.ProfitEvent{OperationID: o.ID, LedgerID: ledger.ID, InstrumentKey: o.InstrumentKey, Currency: currency, Kind: kind, Amount: value, Reason: reason, At: o.OccurredAt})
	}
	for _, o := range ops {
		if o.Status == "voided" {
			continue
		}
		if o.Status != "posted" || o.ID == "" || ids[o.ID] || o.Sequence < 1 || sequences[o.Sequence] || o.LedgerID != ledger.ID || o.TenantID != ledger.TenantID {
			return result, invalid("unique scoped portfolio operation required")
		}
		ids[o.ID], sequences[o.Sequence] = true, true
		if o.ImportedCashDeficitReason != "" {
			if e := validateCashDeficitSource(o); e != nil {
				return result, e
			}
		}
		if o.SettlementAssumed && !o.OccurredAt.After(asOf) && !slices.Contains(result.Snapshot.Issues, "settlement_assumed_trade_time") {
			result.Snapshot.Issues = append(result.Snapshot.Issues, "settlement_assumed_trade_time")
		}
		if cutoff == nil && o.OccurredAt.After(asOf) {
			cutoff = nativeCloneBooks(books)
		}
		if o.InstrumentKey == "" {
			if o.Kind == "fee" {
				event(o, o.Currency, "fees", new(big.Rat).Neg(rational(o.Amount)), "")
			}
			if o.Kind == "cash_interest" {
				event(o, o.Currency, "earned_income", rational(o.Amount), "")
			}
			continue // No funding, cash balance or tax book in a portfolio.
		}
		b := books[o.InstrumentKey]
		if b == nil {
			b = &nativePortfolioBook{realized: map[string]*big.Rat{}, realizedAccrued: map[string]*big.Rat{}, coupons: map[string]*big.Rat{}}
			books[o.InstrumentKey] = b
		}
		if b.currency == "" && o.Currency != "" {
			b.currency = o.Currency
		}
		var before []RemainingLot
		if len(options.origins) > 0 && !o.OccurredAt.After(asOf) {
			for i := range b.lots {
				if b.lots[i].Quantity.Sign() > 0 {
					before = append(before, *cloneRemainingLot(&b.lots[i]))
				}
			}
		}
		principal, accrued, fee := tradeBookValues(o)
		quantity := rational(o.Quantity)
		switch o.Kind {
		case "buy", "opening_position":
			if b.nominal == "0" {
				return result, inconsistent("cannot buy a redeemed instrument")
			}
			lot := RemainingLot{Currency: o.Currency, LotID: o.ID, OriginID: o.ID, OriginLedgerID: ledger.ID, OriginSequence: o.Sequence, AcquiredAt: o.OccurredAt, Quantity: quantity, CleanCost: principal, AcquiredAccrued: accrued}
			if o.Kind == "opening_position" {
				lot.AcquiredAt = o.AcquiredAt
				if !o.OpeningCostKnown {
					lot.CleanCost = nil
				}
				if !o.OpeningAccruedKnown {
					lot.AcquiredAccrued = nil
				}
			}
			b.lots = append(b.lots, lot)
			result.SecurityLegs = append(result.SecurityLegs, d.InvestmentSecurityLeg{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Quantity: o.Quantity, EffectiveAt: o.OccurredAt})
		case "sell", "redemption":
			held := nativeQuantity(b)
			if quantity.Cmp(held) > 0 {
				return result, &ReplayOperationError{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Cause: inconsistent("short sale is not supported")}
			}
			if o.Kind == "redemption" {
				if quantity.Cmp(held) != 0 {
					return result, inconsistent("full redemption must close the entire position")
				}
				if o.NominalBefore != "0" && b.nominal != "" && b.nominal != o.NominalBefore {
					return result, inconsistent("prior nominal does not match preceding corporate event")
				}
				b.nominal = "0"
			}
			nativeAdd(b.realized, o.Currency, principal)
			nativeAdd(b.realizedAccrued, o.Currency, accrued)
			gains, interestGains := map[string]*big.Rat{}, map[string]*big.Rat{}
			nativeAdd(gains, o.Currency, principal)
			nativeAdd(interestGains, o.Currency, accrued)
			remaining := new(big.Rat).Set(quantity)
			unknownClean, unknownAccrued := map[string]bool{}, map[string]bool{}
			for i := range b.lots {
				lot := &b.lots[i]
				if remaining.Sign() == 0 {
					break
				}
				if lot.Quantity.Sign() == 0 {
					continue
				}
				take := new(big.Rat).Set(remaining)
				if take.Cmp(lot.Quantity) > 0 {
					take.Set(lot.Quantity)
				}
				share := new(big.Rat).Quo(take, lot.Quantity)
				for _, part := range []struct {
					amount        **big.Rat
					totals, gains map[string]*big.Rat
					unknown       map[string]bool
				}{{&lot.CleanCost, b.realized, gains, unknownClean}, {&lot.AcquiredAccrued, b.realizedAccrued, interestGains, unknownAccrued}} {
					if *part.amount == nil {
						part.unknown[lot.Currency] = true
						continue
					}
					consumed := new(big.Rat).Mul(*part.amount, share)
					(*part.amount).Sub(*part.amount, consumed)
					consumed.Neg(consumed)
					nativeAdd(part.totals, lot.Currency, consumed)
					nativeAdd(part.gains, lot.Currency, consumed)
				}
				lot.Quantity.Sub(lot.Quantity, take)
				remaining.Sub(remaining, take)
			}
			for _, part := range []struct {
				values  map[string]*big.Rat
				unknown map[string]bool
				kind    string
			}{{gains, unknownClean, "realized_clean"}, {interestGains, unknownAccrued, "realized_accrued"}} {
				currencies := map[string]bool{}
				for c := range part.values {
					currencies[c] = true
				}
				for c := range part.unknown {
					currencies[c] = true
				}
				keys := make([]string, 0, len(currencies))
				for c := range currencies {
					keys = append(keys, c)
				}
				slices.Sort(keys)
				for _, c := range keys {
					if part.unknown[c] {
						event(o, c, part.kind, nil, "unknown_acquisition_cost")
					} else {
						event(o, c, part.kind, part.values[c], "")
					}
				}
			}
			b.realizedUnknown = b.realizedUnknown || len(unknownClean) > 0
			b.accruedUnknown = b.accruedUnknown || len(unknownAccrued) > 0
			result.SecurityLegs = append(result.SecurityLegs, d.InvestmentSecurityLeg{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Quantity: signed(o.Quantity, true), EffectiveAt: o.OccurredAt})
		case "split":
			beforeQuantity := nativeQuantity(b)
			ratio := new(big.Rat).Quo(rational(o.SplitNew), rational(o.SplitOld))
			for i := range b.lots {
				b.lots[i].Quantity.Mul(b.lots[i].Quantity, ratio)
			}
			delta := new(big.Rat).Sub(nativeQuantity(b), beforeQuantity)
			if delta.Sign() != 0 {
				result.SecurityLegs = append(result.SecurityLegs, d.InvestmentSecurityLeg{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Quantity: decimalString(delta), EffectiveAt: o.OccurredAt})
			}
		case "coupon", "dividend", "repo_income":
			if o.Kind == "coupon" {
				nativeAdd(b.coupons, o.Currency, rational(o.Amount))
			}
			event(o, o.Currency, "earned_income", rational(o.Amount), "")
		}
		if (o.Kind == "buy" || o.Kind == "sell") && fee.Sign() != 0 {
			currency := o.Currency
			if o.TradeFinancials != nil {
				currency = o.TradeFinancials.Fee.Currency
				fee = rational(o.TradeFinancials.Fee.Amount)
			} else if o.FeeCurrency != "" {
				currency = o.FeeCurrency
			}
			feeOperation := o
			if o.TradeFinancials != nil {
				feeOperation.OccurredAt = o.TradeFinancials.Fee.At
			}
			event(feeOperation, currency, "fees", new(big.Rat).Neg(fee), "")
		}
		if !o.OccurredAt.After(asOf) && len(options.origins) > 0 {
			old, newLots := map[string]*RemainingLot{}, map[string]*RemainingLot{}
			order := []string{}
			for i := range before {
				lot := &before[i]
				if options.origins[lot.OriginID] {
					old[lot.LotID] = lot
					order = append(order, lot.LotID)
				}
			}
			for i := range b.lots {
				lot := &b.lots[i]
				if options.origins[lot.OriginID] && lot.Quantity.Sign() > 0 {
					newLots[lot.LotID] = lot
					if old[lot.LotID] == nil {
						order = append(order, lot.LotID)
					}
				}
			}
			for _, id := range order {
				left, right := old[id], newLots[id]
				if left != nil && right != nil && sameRemainingLot(left, right) {
					continue
				}
				result.LotHistory = append(result.LotHistory, LotChange{LedgerID: ledger.ID, LedgerName: ledger.Name, Sequence: o.Sequence, OperationID: o.ID, Kind: o.Kind, InstrumentKey: o.InstrumentKey, At: o.OccurredAt, Before: cloneRemainingLot(left), After: cloneRemainingLot(right)})
				if len(result.LotHistory) > 10000 {
					return result, d.ErrInvestmentLimit
				}
			}
		}
		if options.stop == o.ID {
			break
		}
	}
	if cutoff == nil {
		cutoff = books
	}
	keys := make([]string, 0, len(cutoff))
	for key := range cutoff {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	issue := func(code string) {
		if !slices.Contains(result.Snapshot.Issues, code) {
			result.Snapshot.Issues = append(result.Snapshot.Issues, code)
		}
	}
	for _, key := range keys {
		b := cutoff[key]
		currency := b.currency
		if currency == "" {
			continue
		} // A calendar split before any holding.
		currencies := map[string]bool{}
		for _, lot := range b.lots {
			if lot.Quantity.Sign() > 0 {
				currencies[lot.Currency] = true
			}
		}
		if len(currencies) == 1 {
			for c := range currencies {
				currency = c
			}
		}
		value := d.InvestmentPosition{CostBasis: "principal", InstrumentKey: key, Currency: currency, Quantity: decimalString(nativeQuantity(b)), CleanCost: "0", AcquiredAccrued: "0", OutstandingNominal: b.nominal, RealizedClean: nativeScalar(b.realized, currency, b.realizedUnknown), RealizedAccrued: nativeScalar(b.realizedAccrued, currency, b.accruedUnknown), CouponGross: nativeScalar(b.coupons, currency, false), CouponTax: "0", CouponRecovered: "0", CouponEarned: nativeScalar(b.coupons, currency, false)}
		cost, interest := new(big.Rat), new(big.Rat)
		cleanKnown, accruedKnown := true, true
		for _, lot := range b.lots {
			if lot.Quantity.Sign() == 0 {
				continue
			}
			clean, accrued := "", ""
			if lot.CleanCost != nil {
				clean = decimalString(lot.CleanCost)
				cost.Add(cost, lot.CleanCost)
			} else {
				cleanKnown = false
				issue("unknown_acquisition_cost")
			}
			if lot.AcquiredAccrued != nil {
				accrued = decimalString(lot.AcquiredAccrued)
				interest.Add(interest, lot.AcquiredAccrued)
			} else {
				accruedKnown = false
				issue("unknown_acquired_interest")
			}
			if lot.AcquiredAt.IsZero() {
				issue("historical_acquisition_date_unknown")
			}
			value.Lots = append(value.Lots, d.InvestmentLot{Currency: lot.Currency, OriginID: lot.OriginID, AcquisitionOperationID: lot.OriginID, OriginLedgerID: lot.OriginLedgerID, OriginSequence: lot.OriginSequence, Quantity: decimalString(lot.Quantity), CleanCost: clean, AcquiredAccrued: accrued, AcquiredAt: lot.AcquiredAt})
			result.ExactLots[key] = append(result.ExactLots[key], *cloneRemainingLot(&lot))
		}
		if len(currencies) > 1 {
			cleanKnown, accruedKnown = false, false
			issue("mixed_native_cost_currencies")
		}
		if cleanKnown {
			value.CleanCost = decimalString(cost)
		} else {
			value.CleanCost = ""
			cost = nil
		}
		if accruedKnown {
			value.AcquiredAccrued = decimalString(interest)
		} else {
			value.AcquiredAccrued = ""
			interest = nil
		}
		result.ExactPositions[key] = PositionBookValues{Quantity: nativeQuantity(b), CleanCost: cost, AcquiredAccrued: interest}
		result.Snapshot.Positions = append(result.Snapshot.Positions, value)
	}
	return result, nil
}
