package postgres

import (
	"context"
	"math/big"
	"sort"
	"time"

	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

type PortfolioMarketCoverage struct{ RequiredCurrencyDates, FXChecks, FreshQuotes int }

// VerifyPortfolioMarketCoverage checks currencies/dates actually used by the
// saved history, not a few sampled rates or just the existence of FX rows.
func (r *InvestmentRepo) VerifyPortfolioMarketCoverage(ctx context.Context, tenant, actor string, input PortfolioMarketInput) (PortfolioMarketCoverage, error) {
	var out PortfolioMarketCoverage
	svc := inv.NewService(r, NewTenantRepo(r.pool))
	accounts, e := svc.List(ctx, tenant, actor, 200, 0)
	if e != nil {
		return out, e
	}
	loc, e := time.LoadLocation("Europe/Moscow")
	if e != nil {
		return out, e
	}
	needed := map[string]bool{}
	days := map[string]bool{}
	add := func(amount, currency string, at time.Time) {
		value, ok := new(big.Rat).SetString(amount)
		if !ok || value.Sign() == 0 {
			return
		}
		date := at.In(loc).Format(time.DateOnly)
		needed[date+"/"+currency] = true
		days[date] = true
	}
	for _, a := range accounts {
		for offset := 0; ; offset += 200 {
			ops, e := svc.Operations(ctx, tenant, actor, a.ID, 200, offset)
			if e != nil {
				return out, e
			}
			for _, o := range ops {
				if o.Status != "posted" || o.Kind == "split" {
					continue
				}
				if o.TradeFinancials == nil {
					at := o.OccurredAt
					if o.Kind == "opening_position" {
						at = o.AcquiredAt
					}
					add(o.Amount, o.Currency, at)
					add(o.Accrued, o.Currency, at)
					feeCurrency := o.FeeCurrency
					if feeCurrency == "" {
						feeCurrency = o.Currency
					}
					add(o.Fee, feeCurrency, at)
				} else {
					for _, money := range []d.InvestmentTradeMoney{o.TradeFinancials.Principal, o.TradeFinancials.Accrued, o.TradeFinancials.Fee} {
						add(money.Amount, money.Currency, money.At)
					}
				}
			}
			if len(ops) < 200 {
				break
			}
		}
		state, e := svc.Snapshot(ctx, tenant, actor, a.ID, time.Now().UTC())
		if e != nil {
			return out, e
		}
		for _, p := range state.Positions {
			for _, lot := range p.Lots {
				c := lot.Currency
				if c == "" {
					c = p.Currency
				}
				add(lot.CleanCost, c, lot.AcquiredAt)
			}
		}
	}
	out.RequiredCurrencyDates = len(needed)
	for _, currency := range []string{"RUB", "USD", "EUR"} {
		needed["/"+currency] = true
	}
	dates := []string{}
	for date := range days {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	repo := NewMarketDataRepo(r.pool)
	policies := []string{input.AlphaPolicy.Key}
	if len(input.OfficialPoints) > 0 {
		policies = append(policies, input.OfficialPolicy.Key)
	}
	for _, policy := range policies {
		for _, target := range []string{"RUB", "USD", "EUR"} {
			rates, e := repo.InvestmentExchangeRates(ctx, tenant, actor, policy, target, []string{"RUB", "USD", "EUR"}, dates)
			if e != nil {
				return out, e
			}
			found := map[string]bool{}
			for _, rate := range rates.Rates {
				key := rate.Date + "/" + rate.From
				if needed[key] {
					if rate.Rate == nil {
						return out, d.ErrInvestmentPrecondition
					}
					found[key] = true
					out.FXChecks++
				}
			}
			if len(found) != len(needed) {
				return out, d.ErrInvestmentPrecondition
			}
		}
	}
	keys := []string{}
	for _, q := range input.Quotes {
		keys = append(keys, q.Mapping.InstrumentKey)
	}
	quotes, e := repo.InvestmentQuotes(ctx, tenant, actor, keys)
	if e != nil {
		return out, e
	}
	if len(quotes.Quotes) != len(keys) {
		return out, d.ErrInvestmentPrecondition
	}
	for _, q := range quotes.Quotes {
		if q.State != "fresh" || q.CleanPrice == nil {
			return out, d.ErrInvestmentPrecondition
		}
		out.FreshQuotes++
	}
	return out, nil
}
