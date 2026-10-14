package investmentvaluation

import (
	"testing"
	"time"

	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func quoteFixture(kind, unit, nominal string) (md.Mapping, md.SelectedPrice, time.Time) {
	at := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	mapping := md.Mapping{InstrumentKey: "TEST", Version: 1, Periods: []md.MappingPeriod{{Provider: "tinvest", AccessScope: "personal", InstrumentID: "instrument", Kind: kind, Currency: "USD", Unit: unit, Nominal: nominal, NominalCurrency: "USD", LotSize: "100", From: at.Add(-24 * time.Hour)}}}
	price := md.SelectedPrice{State: "fresh", Fact: &calc.PriceFact{InstrumentID: "instrument", SourceKey: "tinvest/personal", Price: "95", Unit: "provider_unspecified", Adjustment: "unadjusted", EffectiveAt: at.Add(-time.Hour), AvailableAt: at.Add(-time.Hour), Complete: true}}
	return mapping, price, at
}
func TestPortfolioQuoteIsPerSecurityAndCleanBondMoneyNeedsNoNKD(t *testing.T) {
	for _, kind := range []string{"share", "etf", "bond"} {
		unit, nominal, want := "currency_per_unit", "", "95"
		if kind == "bond" {
			unit, nominal, want = "percent_of_nominal", "500", "475"
		}
		mapping, price, at := quoteFixture(kind, unit, nominal)
		result, e := NormalizePortfolioQuote(mapping, price, at)
		if e != nil || result.State != "fresh" || result.CleanPrice == nil || result.CleanPrice.RatString() != want || len(result.Issues) != 0 {
			t.Fatal(result, e)
		}
	}
}
func TestPortfolioQuotesNeverPromoteStaleUnknownOrConflictingPriceToValue(t *testing.T) {
	for _, change := range []string{"stale", "missing", "adjustment", "currency", "terms", "zero", "invalid"} {
		mapping, price, at := quoteFixture("share", "currency_per_unit", "")
		switch change {
		case "stale":
			price.State = "stale"
		case "missing":
			price.Fact = nil
		case "adjustment":
			price.Fact.Adjustment = "provider_unspecified"
		case "currency":
			price.Fact.Currency = "RUB"
		case "terms":
			mapping.Periods = nil
		case "zero":
			price.Fact.Price = "0"
		case "invalid":
			price.Fact.Price = "invalid provider amount"
		}
		result, e := NormalizePortfolioQuote(mapping, price, at)
		if e != nil || result.CleanPrice != nil {
			t.Fatal(change, result, e)
		}
	}
}
