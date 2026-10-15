package investmentcalc

import (
	"fmt"
	"math/big"
	"sort"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

// TrackerFlows defines a securities-only strategy: buying is a contribution,
// selling and paid income are withdrawals. It needs no funding/cash ledger.
// Trade principal follows the trade date; independently recorded commissions
// follow their own date/currency. Actual settlement is used when supplied,
// without calculating taxes or inventing currency conversions.
func TrackerFlows(ledgers []string, operations []d.InvestmentOperation, transferValue func(d.InvestmentOperation) (SecurityFlowValue, error)) ([]ExternalFlow, error) {
	selected := map[string]bool{}
	for _, id := range ledgers {
		if id == "" || selected[id] {
			return nil, fmt.Errorf("unique ledgers required")
		}
		selected[id] = true
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("selected ledgers required")
	}
	seen := map[string]bool{}
	out := []ExternalFlow{}
	transfers := []d.InvestmentOperation{}
	parse := func(s string) (*big.Rat, error) {
		if s == "" {
			s = "0"
		}
		v, e := number(s)
		if e != nil || v.Sign() < 0 {
			return nil, fmt.Errorf("nonnegative recorded money required")
		}
		return v, nil
	}
	add := func(o d.InvestmentOperation, kind, currency, amount string, at time.Time, sign int) error {
		v, e := parse(amount)
		if e != nil {
			return e
		}
		if v.Sign() == 0 {
			return nil
		}
		if !validMoneyCurrency(currency) || at.IsZero() {
			return fmt.Errorf("dated native money required")
		}
		if sign < 0 {
			v.Neg(v)
		}
		out = append(out, ExternalFlow{OperationID: o.ID, LedgerID: o.LedgerID, InstrumentKey: o.InstrumentKey, Kind: kind, Currency: currency, At: at, Amount: v})
		return nil
	}
	for _, o := range operations {
		if !selected[o.LedgerID] {
			return nil, fmt.Errorf("operation outside selected ledgers")
		}
		if o.Status == "voided" {
			continue
		}
		if o.Status != "posted" || o.ID == "" || seen[o.ID] || o.OccurredAt.IsZero() {
			return nil, fmt.Errorf("unique posted facts required")
		}
		seen[o.ID] = true
		switch o.Kind {
		case "buy", "sell":
			sign := 1
			if o.Kind == "sell" {
				sign = -1
			}
			if f := o.TradeFinancials; f != nil {
				if f.ActualSettlement == "" {
					return nil, fmt.Errorf("actual settlement required")
				}
				if e := add(o, "trade", f.Principal.Currency, f.ActualSettlement, o.OccurredAt, sign); e != nil {
					return nil, e
				}
				// An expense belongs to main settlement only with the same
				// native currency/instant and the explicit inclusion flag.
				if !f.ActualSettlementIncludesExpenses || f.Fee.Currency != f.Principal.Currency || !f.Fee.At.Equal(f.Principal.At) {
					if e := add(o, "commission", f.Fee.Currency, f.Fee.Amount, f.Fee.At, 1); e != nil {
						return nil, e
					}
				}
				if !f.ActualSettlementIncludesExpenses || f.Tax.Currency != f.Principal.Currency || !f.Tax.At.Equal(f.Principal.At) {
					v, e := parse(f.Tax.Amount)
					if e != nil {
						return nil, e
					}
					if v.Sign() != 0 {
						out = append(out, ExternalFlow{OperationID: o.ID, LedgerID: o.LedgerID, InstrumentKey: o.InstrumentKey, Kind: "unmodeled_expense", Currency: o.Currency, At: o.OccurredAt, Reason: "recorded_expense_unmodeled"})
					}
				}
			} else {
				if e := add(o, "trade", o.Currency, o.Amount, o.OccurredAt, sign); e != nil {
					return nil, e
				}
				if e := add(o, "acquired_interest", o.Currency, o.Accrued, o.OccurredAt, sign); e != nil {
					return nil, e
				}
				currency := o.FeeCurrency
				if currency == "" {
					currency = o.Currency
				}
				if e := add(o, "commission", currency, o.Fee, o.OccurredAt, 1); e != nil {
					return nil, e
				}
			}
		case "dividend", "coupon", "repo_income", "cash_interest", "amortization", "redemption":
			if e := add(o, "paid_income_or_proceeds", o.Currency, o.Amount, o.OccurredAt, -1); e != nil {
				return nil, e
			}
		case "fee":
			if e := add(o, "commission", o.Currency, o.Amount, o.OccurredAt, 1); e != nil {
				return nil, e
			}
		case "security_transfer_in", "security_transfer_out":
			transfers = append(transfers, o)
		case "opening_position":
			out = append(out, ExternalFlow{OperationID: o.ID, LedgerID: o.LedgerID, InstrumentKey: o.InstrumentKey, Kind: o.Kind, Currency: o.Currency, At: o.OccurredAt, Reason: "history_reset"})
		case "split", "distribution", "deposit", "withdrawal", "opening_cash", "cash_reconciliation", "transfer_in", "transfer_out", "fx", "tax", "tax_refund":
			// Corporate units and budget cash are not strategy funding. Amounts
			// already reported in actual settlement/payouts are not taxed again.
		default:
			return nil, fmt.Errorf("unsupported strategy operation")
		}
	}
	security, e := ExternalFlowsWithSecurities(ledgers, transfers, transferValue)
	if e != nil {
		return nil, e
	}
	out = append(out, security...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].OperationID < out[j].OperationID
		}
		return out[i].At.Before(out[j].At)
	})
	return out, nil
}
