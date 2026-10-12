package investment

import (
	"math/big"
	"sort"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

// Growth measures recorded net income, without mixing currencies, principal,
// fees or tax refunds. It makes no inference about dividends per share.
type PayoutGrowth struct {
	Month, PreviousMonth, Currency, Status              string
	IncomeNet, PreviousIncomeNet, Change, PercentChange *big.Rat
}

type payoutGrowthBuilder struct {
	q           PayoutQuery
	cutoff      time.Time
	loc         *time.Location
	ledgers     map[string]d.InvestmentLedger
	first, last time.Time
	values      map[string]*big.Rat
	currencies  map[string]bool
}

func newPayoutGrowth(q PayoutQuery, cutoff time.Time, loc *time.Location, ledgers map[string]d.InvestmentLedger) *payoutGrowthBuilder {
	first, _ := time.ParseInLocation(time.DateOnly, q.FromDate, loc)
	last, _ := time.ParseInLocation(time.DateOnly, q.ToDate, loc)
	first = time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, loc)
	last = time.Date(last.Year(), last.Month(), 1, 0, 0, 0, 0, loc)
	return &payoutGrowthBuilder{q: q, cutoff: cutoff, loc: loc, ledgers: ledgers, first: first, last: last, values: map[string]*big.Rat{}, currencies: map[string]bool{}}
}

func (b *payoutGrowthBuilder) add(e PayoutEntry) {
	switch e.Kind {
	case "dividend", "coupon", "repo_income", "cash_interest":
	default:
		return
	}
	if e.At.Before(b.first.AddDate(-1, 0, 0)) || !e.At.Before(b.last.AddDate(0, 1, 0)) || e.At.After(b.cutoff) {
		return
	}
	month := time.Date(e.At.In(b.loc).Year(), e.At.In(b.loc).Month(), 1, 0, 0, 0, 0, b.loc)
	if (month.Before(b.first) || month.After(b.last)) && (month.Before(b.first.AddDate(-1, 0, 0)) || month.After(b.last.AddDate(-1, 0, 0))) {
		return
	}
	key := month.Format("2006-01") + ":" + e.Currency
	if b.values[key] == nil {
		b.values[key] = new(big.Rat)
	}
	b.values[key].Add(b.values[key], e.Net)
	b.currencies[e.Currency] = true
}

func (b *payoutGrowthBuilder) value(month, currency string) *big.Rat {
	if v := b.values[month+":"+currency]; v != nil {
		return new(big.Rat).Set(v)
	}
	return new(big.Rat)
}

func (b *payoutGrowthBuilder) build() []PayoutGrowth {
	currencies := []string{}
	for c := range b.currencies {
		currencies = append(currencies, c)
	}
	sort.Strings(currencies)
	result := []PayoutGrowth{}
	for month := b.first; !month.After(b.last); month = month.AddDate(0, 1, 0) {
		previous := month.AddDate(-1, 0, 0)
		end := month.AddDate(0, 1, 0).Add(-time.Nanosecond)
		status := "available"
		if b.q.FromDate > month.Format(time.DateOnly) || b.q.ToDate < end.Format(time.DateOnly) || b.cutoff.Before(end) {
			status = "partial_interval"
		}
		if status == "available" {
			for _, l := range b.ledgers {
				if l.StartedAt.After(previous) {
					status = "incomplete_history"
					break
				}
			}
		}
		for _, currency := range currencies {
			g := PayoutGrowth{Month: month.Format("2006-01"), PreviousMonth: previous.Format("2006-01"), Currency: currency, Status: status}
			g.IncomeNet = b.value(g.Month, currency)
			g.PreviousIncomeNet = b.value(g.PreviousMonth, currency)
			if status == "available" {
				g.Change = new(big.Rat).Sub(g.IncomeNet, g.PreviousIncomeNet)
				if g.PreviousIncomeNet.Sign() == 0 {
					g.Status = "zero_baseline"
				} else {
					g.PercentChange = new(big.Rat).Mul(new(big.Rat).Quo(g.Change, g.PreviousIncomeNet), big.NewRat(100, 1))
				}
			}
			result = append(result, g)
		}
	}
	return result
}
