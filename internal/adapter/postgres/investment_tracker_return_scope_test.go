package postgres

import (
	d "github.com/positron48/budget/internal/domain"
	"reflect"
	"testing"
	"time"
)

func TestTrackerReturnExclusionsCloseCorporateFamily(t *testing.T) {
	at := time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)
	op := func(a, b string) d.InvestmentOperation {
		return d.InvestmentOperation{Status: "posted", InstrumentKey: a, OccurredAt: at, Distribution: &d.InvestmentDistribution{TargetInstrumentKey: b}}
	}
	inputs := map[string]investmentReplayInput{"ledger": {Operations: []d.InvestmentOperation{op("DD", "Q"), op("DD", "review"), op("other", "separate")}}}
	keys := map[string]bool{"DD": true, "Q": true, "review": true, "other": true, "separate": true}
	got := closeTrackerReturnExclusions([]string{"Q"}, keys, inputs, at)
	if !reflect.DeepEqual(got, []string{"DD", "Q", "review"}) {
		t.Fatal(got)
	}
	if got = closeTrackerReturnExclusions([]string{"Q"}, keys, inputs, at.Add(-time.Second)); !reflect.DeepEqual(got, []string{"Q"}) {
		t.Fatal("future allocation changed earlier scope", got)
	}
	inputs["ledger"].Operations[0].Status = "voided"
	if got = closeTrackerReturnExclusions([]string{"Q"}, keys, inputs, at); !reflect.DeepEqual(got, []string{"Q"}) {
		t.Fatal("voided allocation affected scope", got)
	}
}
