package postgres

import (
	"testing"
	"time"

	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestTrackerAccruedDoesNotBorrowNominalCurrency(t *testing.T) {
	day := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	value := "1445.61"
	f := md.SavedBondFacts{BondFacts: md.BondFacts{Instrument: md.Instrument{ID: "bond"}, From: day, To: day.Add(48 * time.Hour), AsOf: day.Add(36 * time.Hour), Terms: md.BondTerms{Nominal: &md.BondAmount{Amount: "1000", Currency: "USD"}, CurrentAccrued: &md.BondAmount{Amount: value, Currency: "RUB"}}, Accrued: []md.BondAccrued{{Date: day, Value: &value}}}, RecordedAt: day.Add(37 * time.Hour)}
	if got := trackerDatedAccrued([]md.SavedBondFacts{f}, "bond", "USD", day, day.Add(38*time.Hour)); got != nil {
		t.Fatalf("currency-less historical amount labelled USD: %s", got)
	}
}

func TestTrackerAccruedRequiresDatedCurrencyEvidence(t *testing.T) {
	day := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	value := "5"
	for _, tc := range []struct {
		name, explicit, currentCurrency, currentAmount string
		asOfOffset                                     time.Duration
		want                                           bool
	}{
		{"explicit historical USD", "USD", "RUB", "999", 36 * time.Hour, true},
		{"explicit historical RUB", "RUB", "USD", "5", 36 * time.Hour, false},
		{"same-day matching current ACI", "", "USD", "5.00", 12 * time.Hour, true},
		{"same-day current ACI in RUB", "", "RUB", "5", 12 * time.Hour, false},
		{"same-day different current amount", "", "USD", "6", 12 * time.Hour, false},
		{"current ACI cannot establish yesterday", "", "USD", "5", 36 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := md.SavedBondFacts{BondFacts: md.BondFacts{Instrument: md.Instrument{ID: "bond"}, From: day, To: day.Add(48 * time.Hour), AsOf: day.Add(tc.asOfOffset), Terms: md.BondTerms{Nominal: &md.BondAmount{Amount: "1000", Currency: "USD"}, CurrentAccrued: &md.BondAmount{Amount: tc.currentAmount, Currency: tc.currentCurrency}}, Accrued: []md.BondAccrued{{Date: day, Value: &value, ValueCurrency: tc.explicit}}}, RecordedAt: day.Add(37 * time.Hour)}
			got := trackerDatedAccrued([]md.SavedBondFacts{f}, "bond", "USD", day, day.Add(38*time.Hour))
			if (got != nil) != tc.want || got != nil && got.RatString() != "5" {
				t.Fatalf("unexpected accrued: %v", got)
			}
		})
	}
}
