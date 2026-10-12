package investmentcalc

import (
	"fmt"
	"time"
)

// PositionValueInput describes units held at the cutoff, not broker lots.
// Currency is the expected valuation currency, including when terms are missing.
// Selection of quotes, effective terms and historical FX remains the caller's job.
type PositionValueInput struct {
	ID, Currency, Quantity string
	Quote                  *PriceFact
	Terms                  *PriceTerms
	Accrued                *AccruedFact
}

type CashValueInput struct {
	ID, Currency string
	Cash         Cash
	Known        bool
}

type PortfolioValue struct {
	Valuation
	Positions map[string]NormalizedPrice
}

// ValuePortfolio combines normalized positions and economic cash without an
// intermediate decimal conversion. Missing dirty prices exclude the whole
// position from KnownValue; its known clean component remains in Positions.
// Complete only describes the supplied snapshot: coverage, staleness and missing
// ledger events must be established upstream before presenting a complete NAV.
func ValuePortfolio(positions []PositionValueInput, cash []CashValueInput, fx []FXFixing, target, policy string, at time.Time) (PortfolioValue, error) {
	result := PortfolioValue{Positions: map[string]NormalizedPrice{}}
	components := make([]ExactComponent, 0, len(positions)+len(cash))
	issues := []QualityIssue{}
	seen := map[string]bool{}
	check := func(id, currency string) error {
		if id == "" || seen[id] || !validMoneyCurrency(currency) {
			return fmt.Errorf("unique component and valid currency required")
		}
		seen[id] = true
		return nil
	}
	for _, p := range positions {
		if err := check(p.ID, p.Currency); err != nil {
			return result, err
		}
		quantity, err := number(p.Quantity)
		if err != nil || quantity.Sign() < 0 {
			return result, fmt.Errorf("nonnegative position quantity required")
		}
		// Closed positions require no price or accrued interest to contribute zero.
		if quantity.Sign() == 0 {
			components = append(components, ExactComponent{p.ID, p.Currency, quantity})
			continue
		}
		if p.Terms != nil && p.Terms.Currency != p.Currency {
			return result, fmt.Errorf("position currency conflicts with terms")
		}
		price, err := NormalizePrice(p.Quote, p.Terms, p.Accrued, at)
		if err != nil {
			return result, err
		}
		value, err := price.ValueUnits(p.Quantity)
		if err != nil {
			return result, err
		}
		result.Positions[p.ID] = value
		components = append(components, ExactComponent{p.ID, p.Currency, value.Dirty})
		for _, code := range value.Issues {
			issues = append(issues, QualityIssue{p.ID, code})
		}
	}
	for _, c := range cash {
		if err := check(c.ID, c.Currency); err != nil {
			return result, err
		}
		component := ExactComponent{ID: c.ID, Currency: c.Currency}
		if c.Known {
			amount, err := c.Cash.Economic()
			if err != nil {
				return result, err
			}
			component.Amount = amount
		}
		components = append(components, component)
	}
	valuation, err := ValueExact(components, fx, target, policy, at)
	if err != nil {
		return result, err
	}
	// Replace generic missing-value diagnostics with precise normalization causes.
	explained := map[string]bool{}
	for _, issue := range issues {
		explained[issue.ComponentID] = true
	}
	for _, issue := range valuation.Issues {
		if issue.Code != "value_missing" || !explained[issue.ComponentID] {
			issues = append(issues, issue)
		}
	}
	valuation.Issues = issues
	valuation.Complete = valuation.Complete && len(issues) == 0
	result.Valuation = valuation
	return result, nil
}
