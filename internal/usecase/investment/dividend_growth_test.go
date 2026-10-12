package investment

import (
	"github.com/google/uuid"
	"testing"
	"time"
)

func dividendFixture() DividendHistory {
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	h := DividendHistory{ID: uuid.NewString(), InstrumentKey: "STOCK", Currency: "USD", Timezone: "Europe/Moscow", BasisAt: at("2025-02-01T00:00:00Z"), ReviewedFrom: at("2023-01-01T00:00:00Z"), SourceReference: "Issuer fixture", Evidence: "All unit changes reviewed", Reason: "Fixture"}
	for _, year := range []int{2023, 2024} {
		h.Years = append(h.Years, DividendYearReview{year, true, "Annual statement", "Complete calendar year"})
	}
	h.Dividends = []DividendDeclaration{{uuid.NewString(), "2023-06-01", "10", "regular", at("2023-06-01T00:00:00Z"), "Issuer", "Declared old units", "paid"}, {uuid.NewString(), "2024-06-01", "6", "regular", at("2024-06-01T00:00:00Z"), "Issuer", "Declared new units", "paid"}, {uuid.NewString(), "2024-07-01", "100", "special", at("2024-07-01T00:00:00Z"), "Issuer", "Special distribution", "paid"}}
	h.Splits = []DividendUnitSplit{{uuid.NewString(), "2", "1", at("2024-01-01T00:00:00Z"), "Issuer", "2-for-1"}}
	return h
}
func TestDividendGrowthIssuerUnitsSpecialsAndZero(t *testing.T) {
	h := dividendFixture()
	r, e := BuildDividendHistory(h, h.BasisAt)
	if e != nil || len(r.Years) != 2 {
		t.Fatal(e, r)
	}
	g := r.Years[1]
	if g.Status != "available" || g.Regular.RatString() != "6" || g.PreviousRegular.RatString() != "5" || g.PercentChange.RatString() != "20" || g.Special.RatString() != "100" || g.Total.RatString() != "106" || g.SpecialCount != 1 {
		t.Fatal(g)
	}
	// Reverse split after BOTH years adjusts both amounts, preserving growth.
	h.Splits = append(h.Splits, DividendUnitSplit{uuid.NewString(), "1", "3", h.BasisAt, "Issuer", "Reverse split"})
	r, e = BuildDividendHistory(h, h.BasisAt)
	if e != nil || r.Years[1].Regular.RatString() != "18" || r.Years[1].PercentChange.RatString() != "20" {
		t.Fatal(e, r)
	}
	h.Dividends[1].Amount = "0"
	r, _ = BuildDividendHistory(h, h.BasisAt)
	if r.Years[1].PercentChange.RatString() != "-100" {
		t.Fatal(r)
	}
	h.Dividends[0].Amount = "0"
	r, _ = BuildDividendHistory(h, h.BasisAt)
	if r.Years[1].Status != "zero_baseline" || r.Years[1].PercentChange != nil {
		t.Fatal(r)
	}
}
func TestDividendGrowthUnknownAndReviewedCoverage(t *testing.T) {
	for _, tc := range []string{"partial_year", "incomplete_history", "unknown_amount_or_class", "cancelled"} {
		h := dividendFixture()
		asOf := h.BasisAt
		switch tc {
		case "partial_year":
			asOf = time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)
		case "incomplete_history":
			h.Years[0].Complete = false
		case "unknown_amount_or_class":
			h.Dividends[0].Class = "unknown"
		case "cancelled":
			h.Cancelled = true
		}
		r, e := BuildDividendHistory(h, asOf)
		if e != nil || r.Years[1].Status != tc || r.Years[1].PercentChange != nil {
			t.Fatal(tc, e, r)
		}
	}
	h := dividendFixture()
	h.Dividends[0].Amount = ""
	r, _ := BuildDividendHistory(h, h.BasisAt)
	if r.Years[0].Total != nil || r.Years[1].PercentChange != nil {
		t.Fatal(r)
	}
	// Explicitly reviewed empty year is zero, unlike an absent/incomplete year.
	h = dividendFixture()
	h.Dividends = h.Dividends[:1]
	r, _ = BuildDividendHistory(h, h.BasisAt)
	if r.Years[1].PercentChange.RatString() != "-100" {
		t.Fatal(r)
	}
}
func TestDividendGrowthExactBasisBoundaryAndValidation(t *testing.T) {
	h := dividendFixture()
	h.Dividends[1].Amount = "9007199254740993.123456789012"
	h.Dividends[1].UnitBasisAt = h.Splits[0].EffectiveAt
	r, e := BuildDividendHistory(h, h.BasisAt)
	if e != nil || r.Years[1].Regular.FloatString(12) != "9007199254740993.123456789012" {
		t.Fatal(e, r)
	}
	cases := []func(*DividendHistory){func(h *DividendHistory) { h.Splits[0].NewUnits = "0" }, func(h *DividendHistory) { h.Dividends[0].Amount = "-1" }, func(h *DividendHistory) { h.Dividends[0].UnitBasisAt = h.ReviewedFrom.Add(-time.Second) }, func(h *DividendHistory) { h.Years = append(h.Years, h.Years[0]) }, func(h *DividendHistory) { h.Splits = append(h.Splits, h.Splits[0]) }, func(h *DividendHistory) { h.Dividends[0].PaymentDate = "2023-02-30" }, func(h *DividendHistory) { h.Splits[0].Evidence = "" }}
	for i, change := range cases {
		h := dividendFixture()
		change(&h)
		if _, e := NormalizeDividendHistory(h); e == nil {
			t.Fatal("invalid accepted", i)
		}
	}
}

func TestDividendGrowthUnknownSpecialDoesNotObscureOrdinary(t *testing.T) {
	h := dividendFixture()
	h.Dividends[2].Amount = ""
	r, e := BuildDividendHistory(h, h.BasisAt)
	if e != nil {
		t.Fatal(e)
	}
	g := r.Years[1]
	if g.Status != "available" || g.PercentChange.RatString() != "20" || g.Total != nil || g.Special != nil || g.Regular.RatString() != "6" {
		t.Fatal(g)
	}
}

func TestDividendGrowthPaymentDateDoesNotProveIssuerPayout(t *testing.T) {
	h := dividendFixture()
	h.Dividends[1].Status = "announced"
	r, e := BuildDividendHistory(h, h.BasisAt)
	if e != nil || r.Years[1].Regular != nil || r.Years[1].PercentChange != nil || r.Years[1].Status != "unconfirmed_payment" {
		t.Fatal(e, r)
	}
	h.Dividends[1].Status = "cancelled"
	r, e = BuildDividendHistory(h, h.BasisAt)
	if e != nil || r.Years[1].PercentChange.RatString() != "-100" {
		t.Fatal(e, r)
	}
}
