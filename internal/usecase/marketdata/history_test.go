package marketdata

import (
	"errors"
	"testing"
	"time"
)

func TestPlanRequestedHistoryThirtyYearsAndIntradayBoundary(t *testing.T) {
	from := time.Date(1996, 2, 29, 0, 0, 0, 0, time.UTC)
	q := JobRequest{InstrumentID: "11111111-1111-4111-8111-111111111111", Kind: "daily_candle", From: from, To: from.AddDate(30, 0, 0)}
	windows, err := PlanRequestedHistory(q)
	if err != nil || len(windows) != 31 {
		t.Fatal(len(windows), err)
	}
	cursor := q.From
	for _, w := range windows {
		if !w.From.Equal(cursor) || ValidateRequest(w) != nil {
			t.Fatal(w)
		}
		cursor = w.To
	}
	if !cursor.Equal(q.To) {
		t.Fatal("end lost", cursor)
	}
	q.To = q.To.Add(time.Second)
	if _, err = PlanRequestedHistory(q); !errors.Is(err, ErrInvalid) {
		t.Fatal("horizon bypass", err)
	}
	q.To = from.AddDate(7, 0, 0).Add(12 * time.Hour)
	if windows, err = PlanRequestedHistory(q); err != nil || !windows[len(windows)-1].To.Equal(q.To) {
		t.Fatal(windows, err)
	}
	for _, change := range []func(*JobRequest){func(q *JobRequest) { q.Kind = "bond_facts" }, func(q *JobRequest) { q.InstrumentID = "invalid" }, func(q *JobRequest) { q.SearchQuery = "s" }, func(q *JobRequest) { q.From = q.From.Add(time.Nanosecond) }, func(q *JobRequest) { q.To = q.From }} {
		bad := q
		change(&bad)
		if _, err = PlanRequestedHistory(bad); !errors.Is(err, ErrInvalid) {
			t.Fatal(bad, err)
		}
	}
}

func TestPlanHistory(t *testing.T) {
	from := time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	id := "11111111-1111-4111-8111-111111111111"
	jobs, err := PlanHistory(id, from, to)
	if err != nil || len(jobs) != 8 {
		t.Fatal(len(jobs), err)
	}
	next := from
	for _, q := range jobs {
		if q.From != next || q.To.Sub(q.From) > 365*24*time.Hour || ValidateRequest(q) != nil {
			t.Fatal(q)
		}
		next = q.To
	}
	if next != to {
		t.Fatal("range lost")
	}
	for _, interval := range [][2]time.Time{{from, from}, {to, from}, {from.Add(time.Second), to}, {from, to.Add(time.Hour)}, {from.In(time.FixedZone("other", 3600)), to}} {
		if _, err = PlanHistory(id, interval[0], interval[1]); !errors.Is(err, ErrInvalid) {
			t.Fatal(interval, err)
		}
	}
	if _, err = PlanHistory("invalid", from, to); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err = PlanHistory(id, time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}
