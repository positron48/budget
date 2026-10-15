package investmentvaluation

import (
	"testing"
	"time"
)

func TestTrackerHistoryBoundsAndCompletedDates(t *testing.T) {
	now := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	q := TrackerHistoryRequest{LedgerIDs: []string{"11111111-1111-4111-8111-111111111111"}, Currency: "RUB", FXPolicyKey: "market", From: "2026-01-01", To: "2026-01-09"}
	if _, e := NormalizeTrackerHistory(q, now); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*TrackerHistoryRequest){func(q *TrackerHistoryRequest) { q.To = "2026-01-10" }, func(q *TrackerHistoryRequest) { q.From = "2010-01-01" }, func(q *TrackerHistoryRequest) { q.LedgerIDs = append(q.LedgerIDs, q.LedgerIDs[0]) }, func(q *TrackerHistoryRequest) { q.Currency = "GBP" }, func(q *TrackerHistoryRequest) { q.From = "bad" }} {
		bad := q
		change(&bad)
		if _, e := NormalizeTrackerHistory(bad, now); e == nil {
			t.Fatal("invalid history accepted", bad)
		}
	}
}

func TestTrackerGroupsAreAUnionWithoutInheritedAccountFlows(t *testing.T) {
	now := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	id := "11111111-1111-4111-8111-111111111111"
	q := TrackerHistoryRequest{LedgerIDs: []string{id}, Currency: "RUB", FXPolicyKey: "market", From: "2026-01-01", To: "2026-01-09", GroupIDs: []string{id, "", id}}
	normalized, e := NormalizeTrackerHistory(q, now)
	if e != nil || len(normalized.GroupIDs) != 2 || len(q.GroupIDs) != 3 {
		t.Fatal(normalized, e)
	}
	q.PerformanceFrom = q.From
	if _, e = NormalizeTrackerHistory(q, now); e == nil {
		t.Fatal("group inherited whole-account strategy flows")
	}
	q.PerformanceFrom = ""
	q.GroupIDs = []string{"not-a-group-id"}
	if _, e = NormalizeTrackerHistory(q, now); e == nil {
		t.Fatal("malformed group accepted")
	}
}
