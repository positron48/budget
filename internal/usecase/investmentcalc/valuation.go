package investmentcalc

import (
	"fmt"
	"math/big"
	"time"
)

// Cash distinguishes settled money from trade-date obligations. Payables are
// positive liabilities, receivables positive assets; settled cash may be negative.
type Cash struct{ Settled, Receivable, Payable string }

func (c Cash) Economic() (*big.Rat, error) {
	s, err := number(c.Settled)
	if err != nil {
		return nil, err
	}
	r, err := number(c.Receivable)
	if err != nil || r.Sign() < 0 {
		return nil, fmt.Errorf("invalid receivable")
	}
	p, err := number(c.Payable)
	if err != nil || p.Sign() < 0 {
		return nil, fmt.Errorf("invalid payable")
	}
	return new(big.Rat).Sub(new(big.Rat).Add(s, r), p), nil
}

// Component is a native value already calculated for the requested cutoff.
// Unknown values must have Known=false; an absent value never means zero.
type Component struct {
	ID, Currency, Amount string
	Known                bool
}

// FXFixing is explicitly selected by the caller's source policy. Dates and
// timestamps must reflect the provider's historical fixing convention.
type FXFixing struct {
	Withdrawn                   bool `json:",omitempty"`
	From, To, Rate, Policy      string
	ID                          string
	Revision                    int64
	At, AvailableAt, ValidUntil time.Time
}
type QualityIssue struct{ ComponentID, Code string }
type Valuation struct {
	// Exact amounts in the reporting currency, including nil for unknown values.
	ComponentValues map[string]*big.Rat
	KnownValue      *big.Rat
	Complete        bool
	Issues          []QualityIssue
	FXPaths         map[string][]FXLeg
}

// Value selects the latest fixing no later than cutoff under exactly one policy.
// It never falls back to another policy or borrows a future fixing.
func Value(components []Component, fx []FXFixing, target, policy string, cutoff time.Time) (Valuation, error) {
	exact := make([]ExactComponent, 0, len(components))
	for _, c := range components {
		var amount *big.Rat
		if c.Known {
			var err error
			amount, err = number(c.Amount)
			if err != nil {
				return Valuation{}, err
			}
		}
		exact = append(exact, ExactComponent{ID: c.ID, Currency: c.Currency, Amount: amount})
	}
	return ValueExact(exact, fx, target, policy, cutoff)
}

// ExactComponent keeps rational native amounts exact until presentation. Nil
// means unknown; callers retain ownership of the supplied rational.
type ExactComponent struct {
	ID, Currency string
	Amount       *big.Rat
}

func ValueExact(components []ExactComponent, fx []FXFixing, target, policy string, cutoff time.Time) (Valuation, error) {
	result := Valuation{KnownValue: new(big.Rat), Complete: true, Issues: []QualityIssue{}}
	if !validMoneyCurrency(target) || policy == "" || cutoff.IsZero() {
		return result, fmt.Errorf("target, policy and cutoff required")
	}
	rates, err := NewFXResolver(fx, policy, cutoff)
	if err != nil {
		return result, err
	}
	return ValueExactUsingResolver(components, rates, target)
}

// ValueExactUsingResolver reuses a resolver selected at the caller's cutoff.
func ValueExactUsingResolver(components []ExactComponent, rates *FXResolver, target string) (Valuation, error) {
	result := Valuation{KnownValue: new(big.Rat), Complete: true, Issues: []QualityIssue{}}
	if rates == nil || !validMoneyCurrency(target) {
		return result, fmt.Errorf("target and FX resolver required")
	}
	result.FXPaths = map[string][]FXLeg{}
	result.ComponentValues = map[string]*big.Rat{}
	seen := map[string]bool{}
	for _, c := range components {
		if c.ID == "" || seen[c.ID] || !validMoneyCurrency(c.Currency) {
			return result, fmt.Errorf("unique component and currency required")
		}
		seen[c.ID] = true
		result.ComponentValues[c.ID] = nil
		missing := func(code string) {
			result.Complete = false
			result.Issues = append(result.Issues, QualityIssue{c.ID, code})
		}
		if c.Amount == nil {
			missing("value_missing")
			continue
		}
		amount := copyRat(c.Amount)
		if c.Currency != target && amount.Sign() != 0 {
			resolved, ok := rates.Resolve(c.Currency, target)
			if !ok {
				missing("fx_missing")
				continue
			}
			amount.Mul(amount, resolved.Rate)
			result.FXPaths[c.Currency] = resolved.Legs
		}
		result.ComponentValues[c.ID] = copyRat(amount)
		result.KnownValue.Add(result.KnownValue, amount)
	}
	return result, nil
}

// BondValue requires nominal and accrued interest already expressed in the same
// currency under the instrument's contractual terms. Prices are clean percent.
func BondValue(quantity, nominal, percent, accrued string) (*big.Rat, error) {
	values := make([]*big.Rat, 4)
	for i, s := range []string{quantity, nominal, percent, accrued} {
		v, err := number(s)
		if err != nil || v.Sign() < 0 {
			return nil, fmt.Errorf("nonnegative bond inputs required")
		}
		values[i] = v
	}
	clean := new(big.Rat).Quo(new(big.Rat).Mul(values[1], values[2]), big.NewRat(100, 1))
	return new(big.Rat).Mul(values[0], new(big.Rat).Add(clean, values[3])), nil
}
