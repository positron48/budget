package marketdata

import (
	"testing"
	"time"
)

func TestNativeSessionNominalIsBoundToExactObservationRevision(t *testing.T) {
	o := validObservation()
	o.Currency, o.PriceUnit, o.Adjustment = "USD", "percent_of_nominal", "unadjusted"
	o.Nominal, o.NominalCurrency = "100", "USD"
	saved := SavedObservation{Observation: o, ID: "observation", Revision: 2, ReceivedAt: o.At.AddDate(2, 0, 0)}
	facts, err := ObservationBondFacts(saved)
	if err != nil || facts.ID != "observation:2" || facts.Terms.Nominal.Amount != "100.000000000" || facts.Terms.Nominal.Currency != "USD" || len(facts.Accrued) != 1 || !facts.Accrued[0].Date.Equal(o.At) || !facts.RecordedAt.Equal(saved.ReceivedAt) || len(facts.Hash) != 64 {
		t.Fatal(facts, err)
	}
	if facts.Terms.CurrentAccrued != nil || len(facts.Coupons) != 0 || len(facts.Events) != 0 {
		t.Fatal("invented other bond facts", facts)
	}
	saved.Nominal = "80"
	corrected, err := ObservationBondFacts(saved)
	if err != nil || corrected.Hash == facts.Hash || corrected.Terms.Nominal.Amount != "80.000000000" {
		t.Fatal(corrected, err)
	}
	for _, mutate := range []func(*SavedObservation){
		func(o *SavedObservation) { o.NominalCurrency = "RUB" },
		func(o *SavedObservation) { o.Nominal = "0" },
		func(o *SavedObservation) { o.PriceUnit = "currency_per_unit" },
		func(o *SavedObservation) { o.Revision = 0 },
		func(o *SavedObservation) { o.At = o.At.Add(time.Hour) },
		func(o *SavedObservation) { o.ReceivedAt = o.At.Add(-time.Second) },
	} {
		copy := saved
		mutate(&copy)
		if _, err := ObservationBondFacts(copy); err == nil {
			t.Fatal("invalid bound nominal accepted", copy)
		}
	}
	saved.Nominal, saved.NominalCurrency = "", ""
	if f, err := ObservationBondFacts(saved); err != nil || f != nil {
		t.Fatal("absent nominal invented", f, err)
	}
}
