package marketdata

import (
	"testing"
	"time"
)

func TestNativeHistoryRangesUseRawLabelsBeforeEligibilityDates(t *testing.T) {
	date := func(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }
	for _, provider := range []string{"moex", "twelvedata"} {
		scope := Scope{Provider: provider, AccessScope: "personal"}
		m := Mapping{Periods: []MappingPeriod{{Provider: "tinvest", AccessScope: "personal", InstrumentID: "live"}}, HistoricalPeriods: []MappingPeriod{{Provider: provider, AccessScope: "personal", InstrumentID: "raw", From: date("2024-01-02"), To: date("2024-01-11")}}}
		r := MappedHistoryRanges(m, scope, date("2024-01-02"), date("2024-01-10"), date("2024-01-15"), true)
		if len(r) != 1 || r[0].InstrumentID != "raw" || r[0].From != date("2024-01-01") || r[0].To != date("2024-01-10").Add(-time.Second) {
			t.Fatal(provider, r)
		}
		m.HistoricalPeriods[0].From = date("2024-01-02").Add(time.Hour)
		r = MappedHistoryRanges(m, scope, date("2024-01-02"), date("2024-01-10"), date("2024-01-15"), true)
		if len(r) != 1 || r[0].From != date("2024-01-02") {
			t.Fatal("intraday mapping admitted earlier eligibility", provider, r)
		}
	}
}

func TestMappedHistoryRanges(t *testing.T) {
	d := func(day string) time.Time { v, _ := time.Parse(time.DateOnly, day); return v }
	scope := Scope{Provider: "tinvest", AccessScope: "personal"}
	m := Mapping{Periods: []MappingPeriod{
		{Provider: "tinvest", AccessScope: "personal", InstrumentID: "old", From: d("2019-01-01"), To: d("2022-01-01")},
		{Provider: "tinvest", AccessScope: "personal", InstrumentID: "new", From: d("2023-01-01").Add(time.Hour + 500*time.Microsecond)},
		{Provider: "tinvest", AccessScope: "other", InstrumentID: "private", From: d("2019-01-01")},
	}}
	r := MappedHistoryRanges(m, scope, d("2020-01-01"), d("2024-01-05"), d("2026-10-11"), true)
	if len(r) != 2 || r[0].From != d("2019-12-18") || r[0].To != d("2022-01-01").Add(-time.Second) || r[1].From != d("2023-01-01").Add(time.Hour+time.Second) || r[1].To != d("2026-10-11") {
		t.Fatal(r)
	}
	r = MappedHistoryRanges(m, scope, d("2020-01-01"), d("2024-01-05"), d("2026-10-11"), false)
	if r[1].To != d("2024-01-06") {
		t.Fatal("closed position range continued", r)
	}
}

func TestHistoryGapsResumeAndOverlap(t *testing.T) {
	from := time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC)
	want := JobRequest{Kind: "daily_candle", InstrumentID: "11111111-1111-4111-8111-111111111111", From: from, To: from.AddDate(7, 0, 0)}
	first, err := PlanHistoryGaps(want, nil, 2)
	if err != nil || len(first) != 2 {
		t.Fatal(first, err)
	}
	requested := append([]JobRequest(nil), first...)
	for i := 0; i < 10; i++ {
		next, e := PlanHistoryGaps(want, requested, 2)
		if e != nil {
			t.Fatal(e)
		}
		if len(next) == 0 {
			break
		}
		for _, q := range next {
			if ValidateRequest(q) != nil {
				t.Fatal(q)
			}
		}
		requested = append(requested, next...)
	}
	if len(requested) != 8 {
		t.Fatal("bounded continuation lost history", len(requested))
	}
	// Duplicate/overlapping evidence never causes another load or mutates inputs.
	requested = append(requested, first[0], first[1])
	if gaps, e := PlanHistoryGaps(want, requested, 100); e != nil || len(gaps) != 0 {
		t.Fatal(gaps, e)
	}
	extended := want
	extended.To = want.To.AddDate(0, 0, 1)
	gaps, e := PlanHistoryGaps(extended, requested, 100)
	if e != nil || len(gaps) != 1 || gaps[0].From != want.To || gaps[0].To != extended.To {
		t.Fatal("extended entire last year", gaps, e)
	}
	middle := want
	middle.From = from.AddDate(0, 0, 10)
	middle.To = from.AddDate(0, 0, 20)
	gaps, e = PlanHistoryGaps(want, []JobRequest{middle}, 100)
	if e != nil || gaps[0].From != from || gaps[0].To != middle.From || gaps[1].From != middle.To {
		t.Fatal("hole lost", gaps, e)
	}
	bad := middle
	bad.InstrumentID = "22222222-2222-4222-8222-222222222222"
	if _, e = PlanHistoryGaps(want, []JobRequest{bad}, 100); e == nil {
		t.Fatal("other listing evidence accepted")
	}
	if _, e = PlanHistoryGaps(want, nil, 0); e == nil {
		t.Fatal("unbounded invalid request")
	}
}
