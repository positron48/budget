package investmentvaluation

import (
	"testing"
	"time"
)

func TestTrackerInstrumentScopeAndCompletedDates(t *testing.T) {
	now := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	q := TrackerInstrumentRequest{InstrumentKey: "S", Currency: "RUB", From: "2024-01-01", To: "2024-01-03"}
	if _, e := NormalizeTrackerInstrument(q, now); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*TrackerInstrumentRequest){
		func(q *TrackerInstrumentRequest) { q.To = "2026-01-10" },
		func(q *TrackerInstrumentRequest) { q.From = "1899-12-31" },
		func(q *TrackerInstrumentRequest) { q.To = "2024-02-30" },
		func(q *TrackerInstrumentRequest) { q.Currency = "GBP" },
		func(q *TrackerInstrumentRequest) { q.InstrumentKey = "S\n" },
		func(q *TrackerInstrumentRequest) { q.ListingID = "invalid" },
		func(q *TrackerInstrumentRequest) {
			q.Provider = "tinvest"
			q.AccessScope = "personal"
			q.InstrumentID = "11111111-1111-4111-8111-111111111111"
		},
	} {
		bad := q
		change(&bad)
		if _, e := NormalizeTrackerInstrument(bad, now); e == nil {
			t.Fatal("invalid instrument request accepted", bad)
		}
	}
	q.Provider = "tinvest"
	q.AccessScope = "personal"
	q.InstrumentID = "11111111-1111-4111-8111-111111111111"
	q.InstrumentKey = q.Provider + ":" + q.InstrumentID
	if _, e := NormalizeTrackerInstrument(q, now); e != nil {
		t.Fatal(e)
	}
}
