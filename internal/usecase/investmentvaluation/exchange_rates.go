package investmentvaluation

import (
	"math/big"
	"slices"
	"time"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

type ExchangeRate struct {
	Date, From string
	Rate       *big.Rat
}
type ExchangeRates struct {
	Currency, PolicyKey, Mode string
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
			}
			out = append(out, point)
		}
	}
	return out, nil
}
