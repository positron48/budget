package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"slices"
	"strings"
)

func normalizeCashDeficitReason(o *d.InvestmentOperation) error {
	raw := o.ImportedCashDeficitReason
	if raw == "" {
		return nil
	}
	reason := strings.TrimSpace(raw)
	if reason == "" || len(raw) > 2000 {
		return invalid("cash deficit exception requires a reason of at most 2000 bytes")
	}
	if o.TransferID != "" || o.BudgetTransferID != "" || o.CounterpartyLedgerID != "" || strings.Contains(o.Kind, "transfer") || o.Kind == "cash_reconciliation" {
		return invalid("cash deficit exception is not supported for linked transfers or reconciliation")
	}
	o.ImportedCashDeficitReason = reason
	return nil
}

// Source is attached only after the repository checks tenant, row version,
// binding and uniqueness. Replay/archive require that immutable provenance too.
func validateCashDeficitSource(o d.InvestmentOperation) error {
	s := o.Source
	if s == nil || !validID(s.ImportID) || s.Record < 1 || s.Record > 100000 || s.ExpectedVersion < 1 || strings.TrimSpace(s.ReviewReason) == "" || len(s.ReviewReason) > 2000 {
		return invalid("cash deficit exception requires a verified import source")
	}
	return nil
}

type cashDeficit struct{ settled, economic *big.Rat }

func deficit(v *big.Rat) *big.Rat {
	if v != nil && v.Sign() < 0 {
		return new(big.Rat).Neg(v)
	}
	return new(big.Rat)
}
func cashDeficits(cash map[string]map[string]*big.Rat) map[string]cashDeficit {
	out := map[string]cashDeficit{}
	for c, v := range cash {
		economic := new(big.Rat).Sub(new(big.Rat).Add(v["settled"], v["receivable"]), v["payable"])
		out[c] = cashDeficit{deficit(v["settled"]), deficit(economic)}
	}
	return out
}

// Net each reviewed operation atomically. A pending reversal is not additional
// borrowing capacity. Allowance expires at this timestamp and cannot be spent
// by later manual debits, or reused after new funding reduces the deficit.
func cashDeficitAllowance(legs []d.InvestmentCashLeg, currency string, reviewed map[string]bool) cashDeficit {
	byOp := map[string]cashDeficit{}
	for _, l := range legs {
		if l.Currency != currency || !reviewed[l.OperationID] {
			continue
		}
		v, ok := byOp[l.OperationID]
		if !ok {
			v = cashDeficit{new(big.Rat), new(big.Rat)}
		}
		amount := rational(l.Amount)
		if l.Bucket == "settled" {
			v.settled.Add(v.settled, amount)
		}
		if l.Bucket == "payable" {
			v.economic.Sub(v.economic, amount)
		} else {
			v.economic.Add(v.economic, amount)
		}
		byOp[l.OperationID] = v
	}
	out := cashDeficit{new(big.Rat), new(big.Rat)}
	for _, v := range byOp {
		out.settled.Add(out.settled, deficit(v.settled))
		out.economic.Add(out.economic, deficit(v.economic))
	}
	return out
}
func deficitExceeds(balance, prior, allowance *big.Rat) bool {
	if prior == nil {
		prior = new(big.Rat)
	}
	return deficit(balance).Cmp(new(big.Rat).Add(prior, allowance)) > 0
}
func addSnapshotIssue(s *d.InvestmentSnapshot, code string) {
	if !slices.Contains(s.Issues, code) {
		s.Issues = append(s.Issues, code)
	}
}
