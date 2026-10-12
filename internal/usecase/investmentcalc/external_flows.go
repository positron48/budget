package investmentcalc

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

// ExternalFlow is positive for contributions. Opening balances are deliberately
// excluded: they establish NAV, not a fabricated historical investor payment.
type ExternalFlow struct {
	Kind, InstrumentKey, Quantity, ValueSource, Reason, TransferID string `json:",omitempty"`
	OperationID, LedgerID, Currency                                string
	At                                                             time.Time
	Amount                                                         *big.Rat
}

// ExternalFlows consumes the complete, validated journal for selected ledgers.
// Linked internal transfers must have both matching legs; missing evidence is
// an error rather than an invented contribution or withdrawal.
type SecurityFlowValue struct {
	Value                    *big.Rat
	Currency, Date, SourceID string
}

// ExternalFlowsWithSecurities needs reviewed market value on the transfer day
// for a crossing of the selected scope. FIFO cost is never a funding value.
func ExternalFlows(ledgerIDs []string, operations []d.InvestmentOperation) ([]ExternalFlow, error) {
	return ExternalFlowsWithSecurities(ledgerIDs, operations, nil)
}
func ExternalFlowsWithSecurities(ledgerIDs []string, operations []d.InvestmentOperation, value func(d.InvestmentOperation) (SecurityFlowValue, error)) ([]ExternalFlow, error) {
	selected := map[string]bool{}
	for _, id := range ledgerIDs {
		if id == "" || selected[id] {
			return nil, fmt.Errorf("unique ledger IDs required")
		}
		selected[id] = true
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("selected ledgers required")
	}
	seen := map[string]bool{}
	transfers := map[string][]d.InvestmentOperation{}
	out := []ExternalFlow{}
	for _, op := range operations {
		if !selected[op.LedgerID] {
			return nil, fmt.Errorf("operation outside selected ledgers")
		}
		if op.Status == "voided" {
			continue
		}
		if op.Status != "posted" || op.ID == "" || seen[op.ID] || op.OccurredAt.IsZero() {
			return nil, fmt.Errorf("invalid journal operation")
		}
		seen[op.ID] = true
		switch op.Kind {
		case "security_transfer_in", "security_transfer_out":
			q, e := number(op.Quantity)
			if e != nil || q.Sign() <= 0 || op.InstrumentKey == "" || !validMoneyCurrency(op.Currency) || op.TransferID == "" || op.CounterpartyLedgerID == "" || op.CounterpartyLedgerID == op.LedgerID {
				return nil, fmt.Errorf("invalid security flow")
			}
			for _, amount := range []string{op.Amount, op.Accrued, op.Fee, op.Tax} {
				if amount != "" {
					v, e := number(amount)
					if e != nil || v.Sign() != 0 {
						return nil, fmt.Errorf("security transfer must not contain cash")
					}
				}
			}
			if selected[op.CounterpartyLedgerID] {
				transfers[op.TransferID] = append(transfers[op.TransferID], op)
				continue
			}
			flow := ExternalFlow{OperationID: op.ID, LedgerID: op.LedgerID, Currency: op.Currency, At: op.OccurredAt, Kind: op.Kind, TransferID: op.TransferID, InstrumentKey: op.InstrumentKey, Quantity: op.Quantity, Reason: "security_transfer_market_value_missing"}
			if value != nil {
				v, e := value(op)
				if e != nil {
					return nil, e
				}
				if v.Value != nil {
					loc, _ := time.LoadLocation("Europe/Moscow")
					if v.Value.Sign() < 0 || !validMoneyCurrency(v.Currency) || v.Date != op.OccurredAt.In(loc).Format(time.DateOnly) || strings.TrimSpace(v.SourceID) == "" {
						return nil, fmt.Errorf("security flow requires transfer-day market evidence")
					}
					flow.Amount = new(big.Rat).Set(v.Value)
					if op.Kind == "security_transfer_out" {
						flow.Amount.Neg(flow.Amount)
					}
					flow.Currency = v.Currency
					flow.ValueSource = v.SourceID
					flow.Reason = ""
				}
			}
			out = append(out, flow)
		case "cash_reconciliation":
			if op.CashReconciliation == nil || !validMoneyCurrency(op.Currency) {
				return nil, fmt.Errorf("invalid cash reconciliation")
			}
			expected, e := number(op.CashReconciliation.ExpectedSettled)
			if e != nil || expected.Sign() < 0 {
				return nil, fmt.Errorf("invalid expected cash")
			}
			observed, e := number(op.CashReconciliation.ObservedSettled)
			if e != nil || observed.Sign() < 0 {
				return nil, fmt.Errorf("invalid observed cash")
			}
			flow := ExternalFlow{OperationID: op.ID, LedgerID: op.LedgerID, Currency: op.Currency, At: op.OccurredAt, Kind: op.Kind, Reason: "cash_reconciliation_unclassified"}
			if expected.Cmp(observed) == 0 {
				flow.Amount = new(big.Rat)
				flow.Reason = ""
			}
			out = append(out, flow)
		case "deposit", "withdrawal", "transfer_in", "transfer_out":
			amount, err := number(op.Amount)
			if err != nil || amount.Sign() <= 0 || !validMoneyCurrency(op.Currency) {
				return nil, fmt.Errorf("invalid external flow amount")
			}
			if op.Kind == "transfer_in" || op.Kind == "transfer_out" {
				if op.TransferID == "" || op.CounterpartyLedgerID == "" || op.CounterpartyLedgerID == op.LedgerID {
					return nil, fmt.Errorf("unlinked transfer")
				}
				if selected[op.CounterpartyLedgerID] {
					transfers[op.TransferID] = append(transfers[op.TransferID], op)
					continue
				}
			}
			if op.Kind == "withdrawal" || op.Kind == "transfer_out" {
				amount.Neg(amount)
			}
			out = append(out, ExternalFlow{OperationID: op.ID, LedgerID: op.LedgerID, Currency: op.Currency, At: op.OccurredAt, Amount: amount})
		case "opening_cash", "opening_position", "buy", "sell", "dividend", "coupon", "fee", "tax", "tax_refund", "split", "amortization", "redemption", "fx", "cash_interest", "repo_income", "distribution":
		default:
			return nil, fmt.Errorf("unsupported journal operation")
		}
	}
	for _, legs := range transfers {
		if len(legs) != 2 {
			return nil, fmt.Errorf("incomplete internal transfer")
		}
		a, b := legs[0], legs[1]
		securities := a.Kind == "security_transfer_in" || a.Kind == "security_transfer_out"
		if securities {
			if b.Kind != "security_transfer_in" && b.Kind != "security_transfer_out" || a.InstrumentKey != b.InstrumentKey {
				return nil, fmt.Errorf("inconsistent internal security transfer")
			}
			aq, _ := number(a.Quantity)
			bq, _ := number(b.Quantity)
			if aq == nil || bq == nil || aq.Cmp(bq) != 0 {
				return nil, fmt.Errorf("inconsistent internal security quantity")
			}
		} else if b.Kind != "transfer_in" && b.Kind != "transfer_out" {
			return nil, fmt.Errorf("inconsistent internal cash transfer")
		}
		av, bv := new(big.Rat), new(big.Rat)
		if !securities {
			av, _ = number(a.Amount)
			bv, _ = number(b.Amount)
		}
		if a.Kind == b.Kind || a.CounterpartyLedgerID != b.LedgerID || b.CounterpartyLedgerID != a.LedgerID || a.Currency != b.Currency || av.Cmp(bv) != 0 || !a.OccurredAt.Equal(b.OccurredAt) {
			return nil, fmt.Errorf("inconsistent internal transfer")
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].OperationID < out[j].OperationID
		}
		return out[i].At.Before(out[j].At)
	})
	return out, nil
}

// ConvertedFlow retains fixing provenance. Unknown conversion has nil Amount.
type ConvertedFlow struct {
	ExternalFlow
	Date  string
	Value *big.Rat
	FX    []FXLeg
}

// ConvertExternalFlows resolves each flow using the reporting day's historical
// convention supplied by the caller. It never substitutes today's exchange rate.
func ConvertExternalFlows(flows []ExternalFlow, currency string, location *time.Location, resolver func(date string) (*FXResolver, error)) ([]ConvertedFlow, error) {
	if !validMoneyCurrency(currency) || location == nil || resolver == nil {
		return nil, fmt.Errorf("flow conversion convention required")
	}
	out := make([]ConvertedFlow, 0, len(flows))
	for _, flow := range flows {
		if flow.At.IsZero() || !validMoneyCurrency(flow.Currency) {
			return nil, fmt.Errorf("invalid flow")
		}
		point := ConvertedFlow{ExternalFlow: flow, Date: flow.At.In(location).Format(time.DateOnly)}
		if flow.Amount == nil {
			if (flow.Reason != "cash_reconciliation_unclassified" || flow.Kind != "cash_reconciliation") && (flow.Reason != "security_transfer_market_value_missing" || (flow.Kind != "security_transfer_in" && flow.Kind != "security_transfer_out")) {
				return nil, fmt.Errorf("invalid unknown flow")
			}
			out = append(out, point)
			continue
		}
		flow.Amount = new(big.Rat).Set(flow.Amount)
		point.ExternalFlow = flow
		if flow.Currency == currency || flow.Amount.Sign() == 0 {
			point.Value = new(big.Rat).Set(flow.Amount)
		} else {
			fx, err := resolver(point.Date)
			if err != nil {
				return nil, err
			}
			if fx != nil {
				if rate, ok := fx.Resolve(flow.Currency, currency); ok {
					point.Value = new(big.Rat).Mul(flow.Amount, rate.Rate)
					point.FX = rate.Legs
				}
			}
		}
		out = append(out, point)
	}
	return out, nil
}

// DailyExternalFlows returns every day, including known zero-flow days. Callers
// must supply a complete journal for the range; an empty input is not evidence
// that a partial import contains no external payments. One unknown conversion
// makes the day's total unknown even if other payments cancel numerically.
func DailyExternalFlows(from, through string, flows []ConvertedFlow) (map[string]*big.Rat, error) {
	start, err := time.Parse(time.DateOnly, from)
	if err != nil || start.Format(time.DateOnly) != from {
		return nil, fmt.Errorf("invalid flow range")
	}
	end, err := time.Parse(time.DateOnly, through)
	if err != nil || end.Format(time.DateOnly) != through || end.Before(start) || (end.Unix()-start.Unix())/86400 > 3660 {
		return nil, fmt.Errorf("invalid flow range")
	}
	totals := map[string]*big.Rat{}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		totals[day.Format(time.DateOnly)] = new(big.Rat)
	}
	seen := map[string]bool{}
	for _, flow := range flows {
		total, ok := totals[flow.Date]
		if !ok || flow.OperationID == "" || seen[flow.OperationID] {
			return nil, fmt.Errorf("invalid daily flow evidence")
		}
		seen[flow.OperationID] = true
		if flow.Value == nil {
			totals[flow.Date] = nil
		} else if total != nil {
			total.Add(total, flow.Value)
		}
	}
	return totals, nil
}
