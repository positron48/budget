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

func TestReportingCurrentFXEvidenceRetainsInverseAndCrossPaths(t *testing.T) {
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := at.Add(48 * time.Hour)
	fixings := []calc.FXFixing{
		{ID: "usd", Revision: 2, Policy: "market@1", From: "USD", To: "RUB", Rate: "80", At: at, AvailableAt: at.Add(24 * time.Hour), ValidUntil: at.Add(96 * time.Hour)},
		{ID: "eur", Revision: 3, Policy: "market@1", From: "EUR", To: "RUB", Rate: "100", At: at, AvailableAt: at.Add(24 * time.Hour), ValidUntil: at.Add(96 * time.Hour)},
	}
	rates, err := ReportingExchangeRates(fixings, "market@1", "USD", []string{"EUR", "RUB", "USD"}, []string{"2020-01-02"}, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, rate := range rates {
		if rate.Date != "" {
			if len(rate.CurrentLegs) != 0 {
				t.Fatal("historical response grew current evidence", rate)
			}
			continue
		}
		switch rate.From {
		case "EUR":
			if rate.Rate.RatString() != "5/4" || len(rate.CurrentLegs) != 2 || rate.CurrentLegs[0].ID != "eur" || rate.CurrentLegs[0].Inverse || rate.CurrentLegs[1].ID != "usd" || !rate.CurrentLegs[1].Inverse {
				t.Fatal("cross path lost source evidence", rate)
			}
		case "RUB":
			if rate.Rate.RatString() != "1/80" || len(rate.CurrentLegs) != 1 || !rate.CurrentLegs[0].Inverse || !rate.CurrentLegs[0].AvailableAt.Equal(at.Add(24*time.Hour)) || !rate.CurrentLegs[0].At.Equal(at) {
				t.Fatal("inverse path changed fixing time", rate)
			}
		case "USD":
			if rate.Rate.RatString() != "1" || len(rate.CurrentLegs) != 0 {
				t.Fatal("identity rate invented evidence", rate)
			}
		}
	}
	expired, err := ReportingExchangeRates(fixings, "market@1", "RUB", []string{"USD", "EUR"}, nil, at.Add(96*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, rate := range expired {
		if rate.Rate != nil || len(rate.CurrentLegs) != 0 {
			t.Fatal("expired current evidence revived", rate)
		}
	}
}

func TestCurrentFXComparisonUsesPreviousComparableObservation(t *testing.T) {
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	point := func(id, from, to, value string, day int) calc.FXFixing {
		return calc.FXFixing{ID: id, Revision: 1, Policy: "market@1", From: from, To: to, Rate: value, At: at.Add(time.Duration(day) * 24 * time.Hour), AvailableAt: at.Add(time.Duration(day+1) * 24 * time.Hour), ValidUntil: at.Add(time.Duration(day+5) * 24 * time.Hour)}
	}
	old, current := point("old", "USD", "RUB", "80", 0), point("current", "USD", "RUB", "84", 2)
	rates, err := ReportingExchangeRates([]calc.FXFixing{old, current}, "market@1", "RUB", []string{"USD"}, nil, at.Add(80*time.Hour))
	if err != nil || rates[0].PreviousRate == nil || rates[0].PreviousRate.RatString() != "80" || len(rates[0].PreviousLegs) != 1 || rates[0].PreviousLegs[0].ID != "old" {
		t.Fatal(rates, err)
	}
	// Changing the pair orientation must retain the original inverse evidence.
	rates, err = ReportingExchangeRates([]calc.FXFixing{old, current}, "market@1", "USD", []string{"RUB"}, nil, at.Add(80*time.Hour))
	if err != nil || rates[0].PreviousRate.RatString() != "1/80" || !rates[0].PreviousLegs[0].Inverse {
		t.Fatal(rates, err)
	}
	// A withdrawn latest predecessor must not revive an even older observation.
	withdrawn := point("withdrawn", "USD", "RUB", "", 1)
	withdrawn.Withdrawn = true
	rates, err = ReportingExchangeRates([]calc.FXFixing{old, withdrawn, current}, "market@1", "RUB", []string{"USD"}, nil, at.Add(80*time.Hour))
	if err != nil || rates[0].PreviousRate != nil {
		t.Fatal("withdrawn predecessor revived", rates, err)
	}
	// Two-legged comparisons require matching observation dates, not arbitrary older legs.
	eurOld, eurCurrent := point("eur-old", "EUR", "RUB", "100", 0), point("eur-current", "EUR", "RUB", "105", 2)
	rates, err = ReportingExchangeRates([]calc.FXFixing{old, current, eurOld, eurCurrent}, "market@1", "USD", []string{"EUR"}, nil, at.Add(80*time.Hour))
	if err != nil || rates[0].PreviousRate == nil || rates[0].PreviousRate.RatString() != "5/4" || len(rates[0].PreviousLegs) != 2 {
		t.Fatal(rates, err)
	}
	eurOld.At = eurOld.At.Add(time.Hour)
	rates, err = ReportingExchangeRates([]calc.FXFixing{old, current, eurOld, eurCurrent}, "market@1", "USD", []string{"EUR"}, nil, at.Add(80*time.Hour))
	if err != nil || rates[0].PreviousRate != nil {
		t.Fatal("noncomparable cross accepted", rates, err)
	}
}
