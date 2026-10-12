package investmentcalc

import (
	"testing"
	"time"
)

func priceInputs() (PriceFact, PriceTerms, time.Time) {
	at := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	return PriceFact{InstrumentID: "listing", SourceKey: "tenant/provider/scope", Price: "95", Unit: "provider_unspecified", Adjustment: "unadjusted", EffectiveAt: at, AvailableAt: at, Complete: true}, PriceTerms{ID: "terms", Revision: 1, InstrumentID: "listing", SourceKey: "tenant/provider/scope", Kind: "bond", Currency: "RUB", Unit: "percent_of_nominal", Nominal: "1000", NominalCurrency: "RUB", LotSize: "10", From: at.Add(-24 * time.Hour), Confirmed: true}, at
}
func TestNormalizeBondC07AndAmortizedNominal(t *testing.T) {
	quote, terms, at := priceInputs()
	accrued := &AccruedFact{InstrumentID: "listing", SourceID: "issuer", Currency: "RUB", AmountPerUnit: "20", From: at.Add(-time.Hour), To: at.Add(time.Hour)}
	p, err := NormalizePrice(&quote, &terms, accrued, at)
	if err != nil || !p.Complete() {
		t.Fatal(p, err)
	}
	value, err := p.ValueUnits("2")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, value.Clean, "1900")
	equal(t, value.Accrued, "40")
	equal(t, value.Dirty, "1940")
	equal(t, p.Clean, "950")
	terms.Nominal = "800"
	terms.Revision = 2
	p, err = NormalizePrice(&quote, &terms, accrued, at)
	if err != nil || !p.Complete() {
		t.Fatal(p, err)
	}
	equal(t, p.Clean, "760")
	terms.From = at.Add(time.Second)
	p, err = NormalizePrice(&quote, &terms, accrued, at.Add(time.Second))
	if err != nil || p.Clean != nil || p.Complete() {
		t.Fatal("old quote crossed nominal boundary", p, err)
	}
}
func TestNormalizeSharesAndCurrencyNominal(t *testing.T) {
	quote, terms, at := priceInputs()
	terms.Kind = "share"
	terms.Unit = "currency_per_unit"
	terms.LotSize = "100"
	quote.Price = "9007199254740993.000000001"
	p, err := NormalizePrice(&quote, &terms, nil, at)
	if err != nil || !p.Complete() {
		t.Fatal(p, err)
	}
	equal(t, p.Dirty, quote.Price)
	terms.Kind = "currency"
	terms.Unit = "currency_per_base_nominal"
	terms.Nominal = "1000"
	terms.NominalCurrency = "USD"
	quote.Price = "90000"
	p, err = NormalizePrice(&quote, &terms, nil, at)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, p.Clean, "90")
	terms.Nominal = "3"
	quote.Price = "1"
	p, err = NormalizePrice(&quote, &terms, nil, at)
	if err != nil || p.Clean.RatString() != "1/3" {
		t.Fatal("intermediate rounding", p, err)
	}
	value, _ := p.ValueUnits("3")
	equal(t, value.Dirty, "1")
}
func TestNormalizeMissingEvidenceDoesNotInventZero(t *testing.T) {
	quote, terms, at := priceInputs()
	p, err := NormalizePrice(&quote, &terms, nil, at)
	if err != nil || p.Clean == nil || p.Dirty != nil || p.Accrued != nil || p.Complete() {
		t.Fatal(p, err)
	}
	equal(t, p.Clean, "950")
	cases := map[string]func(*PriceFact, *PriceTerms){
		"future close":       func(q *PriceFact, _ *PriceTerms) { q.AvailableAt = at.Add(time.Hour) },
		"adjusted price":     func(q *PriceFact, _ *PriceTerms) { q.Adjustment = "split_adjusted" },
		"unknown adjustment": func(q *PriceFact, _ *PriceTerms) { q.Adjustment = "provider_unspecified" },
		"incomplete candle":  func(q *PriceFact, _ *PriceTerms) { q.Complete = false },
		"currency conflict":  func(q *PriceFact, _ *PriceTerms) { q.Currency = "USD" },
		"unit conflict":      func(q *PriceFact, _ *PriceTerms) { q.Unit = "currency_per_unit" },
		"unconfirmed terms":  func(_ *PriceFact, p *PriceTerms) { p.Confirmed = false },
		"contract FX":        func(_ *PriceFact, p *PriceTerms) { p.NominalCurrency = "USD" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			q, p := quote, terms
			change(&q, &p)
			n, err := NormalizePrice(&q, &p, nil, at)
			if err != nil || n.Clean != nil || n.Dirty != nil || len(n.Issues) == 0 {
				t.Fatal(n, err)
			}
		})
	}
	accrued := &AccruedFact{InstrumentID: "listing", SourceID: "issuer", Currency: "USD", AmountPerUnit: "20", From: at.Add(-time.Hour), To: at.Add(time.Hour)}
	p, err = NormalizePrice(&quote, &terms, accrued, at)
	if err != nil || p.Clean == nil || p.Dirty != nil {
		t.Fatal("cross-currency accrued used", p, err)
	}
	accrued.Currency = "RUB"
	accrued.To = at
	p, err = NormalizePrice(&quote, &terms, accrued, at)
	if err != nil || p.Dirty != nil {
		t.Fatal("expired accrued used", p, err)
	}
	accrued.To = at.Add(time.Hour)
	accrued.AmountPerUnit = "0"
	p, err = NormalizePrice(&quote, &terms, accrued, at)
	if err != nil || !p.Complete() {
		t.Fatal("confirmed zero must be known", p, err)
	}
	equal(t, p.Dirty, "950")
	quote.SourceKey = "another-tenant/provider/scope"
	if _, err = NormalizePrice(&quote, &terms, accrued, at); err == nil {
		t.Fatal("source mismatch accepted")
	}
}
