package investmentvaluation

import (
	"math/big"
	"slices"
	"time"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

type ExchangeRate struct {
	Date, From   string
	Rate         *big.Rat
	CurrentLegs  []calc.FXLeg
	PreviousRate *big.Rat
	PreviousLegs []calc.FXLeg
}
type ExchangeRates struct {
	Currency, PolicyKey, Mode string
	Source, TimeConvention    string
	FXFixings                 []calc.FXFixing
	PolicyVersion             int64
	ReadAt                    time.Time
	Rates                     []ExchangeRate
}

// ExchangeRateDates validates a bounded reporting request. The empty date is
// the actual read instant, not the end of today or a future close.
func ExchangeRateDates(target string, currencies, dates []string, now time.Time) ([]string, []string, error) {
	valid := func(c string) bool { return c == "RUB" || c == "USD" || c == "EUR" }
	if !valid(target) || len(currencies) > 3 || len(dates) > 4096 || now.IsZero() {
		return nil, nil, d.ErrInvestmentInvalid
	}
	sources := slices.Clone(currencies)
	slices.Sort(sources)
	sources = slices.Compact(sources)
	for _, c := range sources {
		if !valid(c) {
			return nil, nil, d.ErrInvestmentInvalid
		}
	}
	days := append(slices.Clone(dates), "")
	slices.Sort(days)
	days = slices.Compact(days)
	loc, _ := time.LoadLocation("Europe/Moscow")
	today := now.In(loc).Format(time.DateOnly)
	for _, date := range days {
		if date == "" {
			continue
		}
		if _, e := DayCutoff(date); e != nil || date < "1900-01-01" || date > today {
			return nil, nil, d.ErrInvestmentInvalid
		}
	}
	return sources, days, nil
}

func ReportingExchangeRates(fixings []calc.FXFixing, policy string, target string, currencies, dates []string, now time.Time) ([]ExchangeRate, error) {
	sources, days, e := ExchangeRateDates(target, currencies, dates, now)
	if e != nil {
		return nil, e
	}
	out := make([]ExchangeRate, 0, len(sources)*len(days))
	for _, date := range days {
		at := now
		if date != "" {
			at, _ = DayCutoff(date)
			if at.After(now) {
				at = now
			}
		}
		resolver, e := calc.NewFXResolver(fixings, policy, at)
		if e != nil {
			return nil, e
		}
		for _, from := range sources {
			point := ExchangeRate{Date: date, From: from}
			if rate, ok := resolver.Resolve(from, target); ok {
				point.Rate = rate.Rate
				if date == "" {
					point.CurrentLegs = rate.Legs
					point.PreviousRate, point.PreviousLegs = previousComparableFX(fixings, rate.Legs, now)
				}
			}
			out = append(out, point)
		}
	}
	return out, nil
}

// Compare the latest earlier observation of the same ordered source pairs.
// This is retrospective evidence under the current policy, not a claim that
// the provider published a value at a historically proven session boundary.
// Revisions of the same observation never count as a previous session. Cross
// paths require aligned observation times on both sides; missing/withdrawn
// predecessors stay unknown rather than reviving older rates.
func previousComparableFX(fixings []calc.FXFixing, current []calc.FXLeg, now time.Time) (*big.Rat, []calc.FXLeg) {
	if len(current) == 0 || len(current) > 2 {
		return nil, nil
	}
	previous := make([]calc.FXFixing, len(current))
	comparisonAt := time.Time{}
	for i, leg := range current {
		if !leg.At.Equal(current[0].At) {
			return nil, nil
		}
		for _, f := range fixings {
			if f.Policy != leg.Policy || f.From != leg.From || f.To != leg.To || !f.At.Before(leg.At) || f.AvailableAt.After(now) {
				continue
			}
			old := previous[i]
			if old.ID == "" || f.At.After(old.At) || (f.At.Equal(old.At) && (f.ID < old.ID || (f.ID == old.ID && f.Revision > old.Revision))) {
				previous[i] = f
			}
		}
		p := previous[i]
		if p.ID == "" || p.Withdrawn || (i > 0 && !p.At.Equal(previous[0].At)) {
			return nil, nil
		}
		if p.AvailableAt.After(comparisonAt) {
			comparisonAt = p.AvailableAt
		}
	}
	product := big.NewRat(1, 1)
	legs := make([]calc.FXLeg, 0, len(current))
	for i, p := range previous {
		if !comparisonAt.Before(p.ValidUntil) {
			return nil, nil
		}
		value, ok := new(big.Rat).SetString(p.Rate)
		if !ok || value.Sign() <= 0 {
			return nil, nil
		}
		if current[i].Inverse {
			value.Inv(value)
		}
		product.Mul(product, value)
		legs = append(legs, calc.FXLeg{ID: p.ID, Revision: p.Revision, From: p.From, To: p.To, Policy: p.Policy, At: p.At, AvailableAt: p.AvailableAt, ValidUntil: p.ValidUntil, Inverse: current[i].Inverse})
	}
	return product, legs
}
