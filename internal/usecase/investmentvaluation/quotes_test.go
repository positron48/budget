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
		if kind == "bond" {
			mapping.Periods[0].From = price.Fact.EffectiveAt.UTC().Truncate(24 * time.Hour)
		}
		result, e := NormalizePortfolioQuote(mapping, price, at)
		if e != nil || result.State != "fresh" || result.CleanPrice == nil || result.CleanPrice.RatString() != want || len(result.Issues) != 0 {
			t.Fatal(result, e)
		}
	}
}

func portfolioBondFixture() (md.Mapping, md.SelectedPrice, time.Time, md.SavedBondFacts) {
	mapping, price, at := quoteFixture("bond", "percent_of_nominal", "500")
	day := at.Truncate(24 * time.Hour)
	nominal := "300"
	facts := md.SavedBondFacts{ID: "saved-facts", Hash: "verified-payload", RecordedAt: at.Add(-time.Minute), BondFacts: md.BondFacts{
		Instrument: md.Instrument{ID: "instrument", Kind: "bond"}, From: day, To: day.Add(24 * time.Hour), AsOf: at.Add(-time.Minute),
		Terms: md.BondTerms{Nominal: &md.BondAmount{Amount: "300", Currency: "USD"}}, Accrued: []md.BondAccrued{{Date: day, Nominal: &nominal}},
	}}
	return mapping, price, at, facts
}

func TestPortfolioPercentageQuoteUsesNominalForQuoteDayAfterAmortization(t *testing.T) {
	mapping, price, at, facts := portfolioBondFixture()
	missing, e := NormalizePortfolioQuote(mapping, price, at)
	if e != nil || missing.CleanPrice != nil || missing.State != "unavailable" {
		t.Fatal("old face value reused", missing, e)
	}
	actual, e := NormalizePortfolioQuoteWithBondFacts(mapping, price, at, &facts)
	if e != nil || actual.CleanPrice == nil || actual.CleanPrice.RatString() != "285" {
		t.Fatal("amortization ignored", actual, e)
	}
	if mapping.Periods[0].Nominal != "500" {
		t.Fatal("valuation rewrote reviewed mapping")
	}
	// A weekend read can use Friday's quote and Friday's nominal, never Monday's.
	later := at.Add(48 * time.Hour)
	facts.AsOf, facts.RecordedAt = later.Add(-time.Minute), later.Add(-time.Minute)
	facts.Terms.Nominal.Amount = "200"
	actual, e = NormalizePortfolioQuoteWithBondFacts(mapping, price, later, &facts)
	if e != nil || actual.CleanPrice == nil || actual.CleanPrice.RatString() != "285" {
		t.Fatal("historical nominal replaced by current contract value", actual, e)
	}
}

func TestPortfolioBondNominalCannotBeGuessedFromMissingOrContradictoryEvidence(t *testing.T) {
	for _, change := range []string{"missing_day", "missing_nominal", "wrong_currency", "wrong_instrument", "future_snapshot", "future_provider", "old_provider", "zero", "contradiction", "contract_mismatch"} {
		t.Run(change, func(t *testing.T) {
			mapping, price, at, facts := portfolioBondFixture()
			switch change {
			case "missing_day":
				facts.Accrued[0].Date = facts.Accrued[0].Date.Add(-24 * time.Hour)
			case "missing_nominal":
				facts.Accrued[0].Nominal = nil
			case "wrong_currency":
				facts.Terms.Nominal.Currency = "RUB"
			case "wrong_instrument":
				facts.Instrument.ID = "different"
			case "future_snapshot":
				facts.RecordedAt = at.Add(time.Hour)
			case "future_provider":
				facts.AsOf = at.Add(time.Hour)
			case "old_provider":
				facts.AsOf = at.Add(-24 * time.Hour)
			case "zero":
				zero := "0"
				facts.Accrued[0].Nominal = &zero
			case "contradiction":
				other := "200"
				facts.Accrued = append(facts.Accrued, md.BondAccrued{Date: at.Truncate(24 * time.Hour), Nominal: &other})
			case "contract_mismatch":
				facts.Terms.Nominal.Amount = "200"
			}
			result, e := NormalizePortfolioQuoteWithBondFacts(mapping, price, at, &facts)
			if e != nil || result.CleanPrice != nil {
				t.Fatal("unconfirmed nominal became money", result, e)
			}
		})
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

func TestHistoricalBondQuoteUsesRawCandleDayAndCurrentKnowledge(t *testing.T) {
	mapping, price, original, facts := portfolioBondFixture()
	rawDay := original.UTC().Truncate(24 * time.Hour)
	at, readAt := rawDay.Add(36*time.Hour), rawDay.Add(10*24*time.Hour)
	price.TimeConvention = md.RetrospectiveCloseNextUTCDay
	price.Fact.EffectiveAt, price.Fact.AvailableAt = rawDay.Add(24*time.Hour), rawDay.Add(24*time.Hour)
	facts.AsOf, facts.RecordedAt = readAt.Add(-time.Minute), readAt.Add(-time.Minute)
	facts.Terms.Nominal.Amount = "200"
	result, e := NormalizeHistoricalPortfolioQuote(mapping, price, at, readAt, &facts)
	if e != nil || result.CleanPrice == nil || result.CleanPrice.RatString() != "285" {
		t.Fatal("raw candle nominal ignored", result, e)
	}
	current, e := NormalizePortfolioQuoteWithBondFacts(mapping, price, at, &facts)
	if e != nil || current.CleanPrice != nil {
		t.Fatal("historical knowledge leaked into current contract", current, e)
	}
	facts.Accrued[0].Date = rawDay.Add(24 * time.Hour)
	result, e = NormalizeHistoricalPortfolioQuote(mapping, price, at, readAt, &facts)
	if e != nil || result.CleanPrice != nil {
		t.Fatal("next-day nominal borrowed", result, e)
	}
}
