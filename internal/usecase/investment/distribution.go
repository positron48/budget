package investment

import (
	"math/big"
	"slices"
	"strings"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"time"
)

func applyDistribution(books map[string]*position, o d.InvestmentOperation, acquisitionOrder map[string]int) ([]d.InvestmentSecurityLeg, error) {
	terms := o.Distribution
	source := books[o.InstrumentKey]
	if source == nil {
		return nil, inconsistent("distribution requires an existing position")
	}
	target := books[terms.TargetInstrumentKey]
	if target == nil {
		target = &position{cleanUnknown: map[string]bool{}, accruedUnknown: map[string]bool{}, lotDates: map[string]time.Time{}, currency: o.Currency, realized: new(big.Rat), realizedAccrued: new(big.Rat)}
	}
	if source.currency != o.Currency || target.currency != o.Currency {
		return nil, inconsistent("distribution cost currency mismatch")
	}
	// Bond entitlement and accrued-interest allocation need their own confirmed
	// terms. Do not silently treat an equity cost ratio as a bond convention.
	for _, p := range []*position{source, target} {
		_, accrued := p.accrued.Totals()
		if p.nominal != "" || accrued.Sign() != 0 {
			return nil, inconsistent("distribution of bond principal or interest requires explicit terms")
		}
		for _, lot := range p.clean.Lots() {
			if p.lotDates[lot.OriginID].IsZero() || p.accruedUnknown[lot.OriginID] {
				return nil, inconsistent("distribution requires acquisition dates and known accrued costs")
			}
		}
	}
	// Work on owned copies. A failure in either cost book must not leave a
	// half-applied two-instrument action, even within the in-memory replay.
	clone := func(book *calc.Book) *calc.Book { return book.Clone() }
	sc, sa, tc, ta := clone(&source.clean), clone(&source.accrued), clone(&target.clean), clone(&target.accrued)
	incoming, err := sc.Distribute(terms.NewUnits, terms.OldUnits, terms.CostNumerator, terms.CostDenominator, terms.ConsumeSource)
	if err != nil {
		return nil, inconsistent(err.Error())
	}
	interest, err := sa.Distribute(terms.NewUnits, terms.OldUnits, terms.CostNumerator, terms.CostDenominator, terms.ConsumeSource)
	if err != nil {
		return nil, inconsistent(err.Error())
	}
	incomingLots := incoming.Lots()
	dates := map[string]time.Time{}
	order := []string{}
	for _, lot := range target.clean.Lots() {
		order = append(order, lot.OriginID)
		dates[lot.OriginID] = target.lotDates[lot.OriginID]
	}
	for _, lot := range incomingLots {
		value, err := normalize(lot.Quantity.FloatString(10), 28, 10)
		if err != nil || rational(value).Cmp(lot.Quantity) != 0 {
			return nil, inconsistent("distribution quantity requires explicit fractional-share resolution")
		}
		order = append(order, lot.OriginID)
		dates[lot.OriginID] = source.lotDates[lot.OriginID]
	}
	slices.SortFunc(order, func(a, b string) int {
		if c := dates[a].Compare(dates[b]); c != 0 {
			return c
		}
		if acquisitionOrder[a] < acquisitionOrder[b] {
			return -1
		}
		if acquisitionOrder[a] > acquisitionOrder[b] {
			return 1
		}
		return strings.Compare(a, b)
	})
	quantity, _ := incoming.Totals()
	normalized, err := normalize(quantity.FloatString(10), 28, 10)
	if err != nil || rational(normalized).Cmp(quantity) != 0 {
		return nil, inconsistent("distribution quantity exceeds supported precision")
	}
	if err = tc.Absorb(incoming, order); err != nil {
		return nil, inconsistent(err.Error())
	}
	if err = ta.Absorb(interest, order); err != nil {
		return nil, inconsistent(err.Error())
	}
	before, _ := source.clean.Totals()
	source.clean, source.accrued, target.clean, target.accrued = *sc, *sa, *tc, *ta
	if target.lotOrigins == nil {
		target.lotOrigins = map[string]lotOrigin{}
	}
	for _, lot := range incomingLots {
		target.lotOrigins[lot.OriginID] = source.lotOrigins[lot.OriginID]
		target.lotDates[lot.OriginID] = source.lotDates[lot.OriginID]
		target.cleanUnknown[lot.OriginID] = source.cleanUnknown[lot.OriginID] && terms.CostNumerator != "0"
		target.accruedUnknown[lot.OriginID] = false
		if terms.CostNumerator == terms.CostDenominator {
			source.cleanUnknown[lot.OriginID] = false
		}
	}
	books[terms.TargetInstrumentKey] = target
	legs := []d.InvestmentSecurityLeg{{OperationID: o.ID, InstrumentKey: terms.TargetInstrumentKey, Quantity: normalized, EffectiveAt: o.OccurredAt}}
	if terms.ConsumeSource {
		legs = append(legs, d.InvestmentSecurityLeg{OperationID: o.ID, InstrumentKey: o.InstrumentKey, Quantity: decimalString(new(big.Rat).Neg(before)), EffectiveAt: o.OccurredAt})
	}
	return legs, nil
}
