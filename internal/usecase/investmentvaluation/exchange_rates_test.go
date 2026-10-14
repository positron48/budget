package investmentvaluation

import (
	"strings"
	"testing"
	"time"

	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

func TestReportingExchangeRatesUseDatedPolicyAndDoNotReviveExpiredRates(t *testing.T) {
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	points := []calc.FXFixing{
		{ID: "old", Revision: 1, Policy: "market@1", From: "USD", To: "RUB", Rate: "80", At: at, AvailableAt: at, ValidUntil: at.Add(48 * time.Hour)},
		{ID: "next", Revision: 1, Policy: "market@1", From: "USD", To: "RUB", Rate: "90", At: at.Add(24 * time.Hour), AvailableAt: at.Add(24 * time.Hour), ValidUntil: at.Add(48 * time.Hour)},
		{ID: "other-policy", Revision: 1, Policy: "cbr@1", From: "USD", To: "RUB", Rate: "70", At: at, AvailableAt: at, ValidUntil: at.Add(72 * time.Hour)},
	}
	rates, e := ReportingExchangeRates(points, "market@1", "RUB", []string{"EUR", "USD", "RUB", "USD"}, []string{"2020-01-01", "2020-01-02", "2020-01-01"}, at.Add(49*time.Hour))
	// Duplicated currencies are rejected before de-duplication: request limits
	// bound the actual input, not only its normalized output.
	if e == nil {
		t.Fatal("oversized currency request accepted", rates)
	}
	rates, e = ReportingExchangeRates(points, "market@1", "RUB", []string{"EUR", "USD", "RUB"}, []string{"2020-01-01", "2020-01-02", "2020-01-01"}, at.Add(49*time.Hour))
	if e != nil || len(rates) != 9 {
		t.Fatal(rates, e)
	}
	for _, r := range rates {
		expected := ""
		switch {
		case r.From == "RUB":
			expected = "1"
		case r.From == "USD" && r.Date == "2020-01-01":
			expected = "80"
		case r.From == "USD" && r.Date == "2020-01-02":
			expected = "90"
		}
		if expected == "" {
			if r.Rate != nil {
				t.Fatal("missing rate became known", r)
			}
		} else if r.Rate == nil || r.Rate.RatString() != expected {
			t.Fatal(r, expected)
		}
	}
}

func TestExchangeRateDatesRejectFutureAndMalformedDates(t *testing.T) {
	now := time.Date(2020, 1, 1, 22, 0, 0, 0, time.UTC) // Jan 2 in Moscow
	for _, date := range []string{"2020-01-03", "bad", "2020-02-30", "1899-12-31", strings.Repeat("x", 100)} {
		if _, _, e := ExchangeRateDates("RUB", []string{"USD"}, []string{date}, now); e == nil {
			t.Fatal(date)
		}
	}
	if _, _, e := ExchangeRateDates("RUB", []string{"USD"}, []string{"2020-01-02"}, now); e != nil {
		t.Fatal(e)
	}
	if _, _, e := ExchangeRateDates("RUB", []string{"USD"}, make([]string, 4097), now); e == nil {
		t.Fatal("unbounded dates accepted")
	}
}
