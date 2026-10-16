package investmentvaluation

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestTrackerSnapshotTamperExpiryKeyAndPrivateSigning(t *testing.T) {
	now := time.Now().UTC()
	key := "synthetic-key-marker"
	s := NewTrackerSnapshotSigner(key)
	q := TrackerHistoryRequest{LedgerIDs: []string{"L"}, Currency: "RUB", FXPolicyKey: "market", From: "2024-01-01", To: "2024-01-02", PerformanceFrom: "2024-01-01"}
	data := TrackerHistory{ReadAt: now, BasisKey: strings.Repeat("a", 64), FXPolicyVersion: 1, FXMode: "market", Points: []TrackerHistoryPoint{{Date: q.From, State: "ready", KnownValue: big.NewRat(10, 1), StrategyFlow: big.NewRat(12, 1)}}}
	token, e := s.Seal("tenant", "actor", q, data)
	if e != nil || strings.Contains(token, key) || strings.Contains(fmt.Sprintf("%v %#v", s, s), key) {
		t.Fatal("snapshot signing/redaction", e)
	}
	claim, e := NewTrackerSnapshotSigner(key).Open(token, now)
	if e != nil || claim.Tenant != "tenant" || claim.Actor != "actor" || claim.Days[0].Flow != "12" {
		t.Fatal(claim, e)
	}
	parts := strings.Split(token, ".")
	parts[1] = "A" + parts[1][1:]
	if _, e = s.Open(strings.Join(parts, "."), now); e == nil {
		t.Fatal("tampered facts accepted")
	}
	if _, e = s.Open(token, now.Add(16*time.Minute)); e == nil {
		t.Fatal("expired snapshot accepted")
	}
	if _, e = NewTrackerSnapshotSigner("other").Open(token, now); e == nil {
		t.Fatal("wrong signing purpose/key accepted")
	}
}

func TestTrackerSnapshotLargeInstrumentScopeUsesDigest(t *testing.T) {
	now := time.Now().UTC()
	q := TrackerHistoryRequest{LedgerIDs: []string{"L"}, Currency: "RUB", FXPolicyKey: "market", From: "2024-01-01", To: "2024-01-02", PerformanceFrom: "2024-01-01"}
	for i := 0; i < 500; i++ {
		q.InstrumentKeys = append(q.InstrumentKeys, fmt.Sprintf("%0198d", i))
	}
	data := TrackerHistory{ReadAt: now, BasisKey: strings.Repeat("a", 64), FXPolicyVersion: 1, FXMode: "market", Points: []TrackerHistoryPoint{{Date: q.From, State: "ready", KnownValue: big.NewRat(10, 1), StrategyFlow: big.NewRat(1, 1)}}}
	signer := NewTrackerSnapshotSigner("synthetic-key")
	token, e := signer.Seal("tenant", "actor", q, data)
	if e != nil || len(token) > 32768 {
		t.Fatal("large bounded scope failed", e)
	}
	p, e := signer.Open(token, now)
	if e != nil || len(p.Request.InstrumentKeys) != 0 || p.InstrumentScopeDigest != TrackerInstrumentScopeDigest(q.InstrumentKeys) {
		t.Fatal("instrument scope digest lost", e)
	}
}

func TestTrackerSnapshotQuarterPageRetainsEveryExactDay(t *testing.T) {
	now := time.Now().UTC()
	signer := NewTrackerSnapshotSigner("synthetic")
	q := TrackerHistoryRequest{PageDays: 93, PerformanceFrom: "2024-01-01"}
	data := TrackerHistory{ReadAt: now}
	first := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 93; i++ {
		data.Points = append(data.Points, TrackerHistoryPoint{Date: first.AddDate(0, 0, i).Format(time.DateOnly), State: "ready", KnownValue: big.NewRat(int64(i), 3), StrategyFlow: big.NewRat(1, 7)})
	}
	token, e := signer.Seal("tenant", "actor", q, data)
	if e != nil {
		t.Fatal(e)
	}
	claim, e := signer.Open(token, now)
	if e != nil || len(claim.Days) != 93 || claim.Days[92].Value != "92/3" {
		t.Fatal(claim, e)
	}
	q.PageDays = 0
	if _, e = signer.Seal("tenant", "actor", q, data); e == nil {
		t.Fatal("legacy page bound bypassed")
	}
}
