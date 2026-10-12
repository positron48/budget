package investmentcalc

import (
	"math/big"
	"testing"
	"time"
)

func TestPortfolioExactNormalizationAndSettlement(t *testing.T) {
	at := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	terms := &PriceTerms{ID: "terms", Revision: 1, InstrumentID: "currency", SourceKey: "source", Kind: "currency", Currency: "USD", Unit: "currency_per_base_nominal", Nominal: "3", NominalCurrency: "EUR", From: at.Add(-time.Hour), Confirmed: true}
	quote := &PriceFact{InstrumentID: "currency", SourceKey: "source", Price: "1", Unit: "provider_unspecified", Adjustment: "unadjusted", EffectiveAt: at, AvailableAt: at, Complete: true}
	positions := []PositionValueInput{{ID: "position", Currency: "USD", Quantity: "3", Terms: terms, Quote: quote}}
	fx := []FXFixing{testFX("USD", "RUB", "90", "market", at)}
	for _, cash := range []Cash{{"100", "0", "2"}, {"98", "0", "0"}} {
		got, err := ValuePortfolio(positions, []CashValueInput{{"cash", "USD", cash, true}}, fx, "RUB", "market", at)
		if err != nil || !got.Complete {
			t.Fatal(got, err)
		}
		equal(t, got.KnownValue, "8910")
		equal(t, got.Positions["position"].Dirty, "1")
	}
}

func TestPortfolioMissingAccruedAndFXRemainIncomplete(t *testing.T) {
	at := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	terms := &PriceTerms{ID: "terms", Revision: 1, InstrumentID: "bond", SourceKey: "source", Kind: "bond", Currency: "RUB", Unit: "percent_of_nominal", Nominal: "1000", NominalCurrency: "RUB", From: at.Add(-time.Hour), Confirmed: true}
	quote := &PriceFact{InstrumentID: "bond", SourceKey: "source", Price: "95", Adjustment: "unadjusted", EffectiveAt: at, AvailableAt: at, Complete: true}
	got, err := ValuePortfolio([]PositionValueInput{{"bond", "RUB", "2", quote, terms, nil}, {ID: "closed", Currency: "USD", Quantity: "0"}}, []CashValueInput{{"rub", "RUB", Cash{"10", "0", "0"}, true}, {"usd", "USD", Cash{"5", "0", "0"}, true}}, []FXFixing{testFX("USD", "RUB", "90", "cbr", at), testFX("USD", "RUB", "91", "market", at.Add(time.Hour))}, "RUB", "market", at)
	if err != nil || got.Complete || len(got.Issues) != 2 {
		t.Fatal(got, err)
	}
	equal(t, got.KnownValue, "10")
	equal(t, got.Positions["bond"].Clean, "1900")
	if got.Positions["bond"].Dirty != nil || got.Issues[0].Code != "accrued_interest_missing" || got.Issues[1].Code != "fx_missing" {
		t.Fatal(got)
	}
}

func TestPortfolioRejectsDoubleCountingAndKeepsUnknownCash(t *testing.T) {
	at := time.Now()
	c := CashValueInput{"cash", "RUB", Cash{"10", "0", "0"}, true}
	if _, err := ValuePortfolio(nil, []CashValueInput{c, c}, nil, "RUB", "market", at); err == nil {
		t.Fatal("duplicate cash accepted")
	}
	if _, err := ValuePortfolio([]PositionValueInput{{ID: "cash", Currency: "RUB", Quantity: "0"}}, []CashValueInput{c}, nil, "RUB", "market", at); err == nil {
		t.Fatal("duplicate position/cash accepted")
	}
	c.Known = false
	got, err := ValuePortfolio(nil, []CashValueInput{c}, nil, "RUB", "market", at)
	if err != nil || got.Complete || len(got.Issues) != 1 {
		t.Fatal(got, err)
	}
	equal(t, got.KnownValue, "0")
}

func TestExactValuationDoesNotMutateInputs(t *testing.T) {
	at := time.Now()
	amount := big.NewRat(1, 3)
	v, err := ValueExact([]ExactComponent{{"value", "USD", amount}}, []FXFixing{testFX("USD", "RUB", "3", "market", at)}, "RUB", "market", at)
	if err != nil || !v.Complete {
		t.Fatal(v, err)
	}
	equal(t, v.KnownValue, "1")
	if amount.Cmp(big.NewRat(1, 3)) != 0 {
		t.Fatal("input changed", amount)
	}
}
