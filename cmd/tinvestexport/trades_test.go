package main

import (
	"encoding/json"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"testing"
	"time"
)

func TestTradeReviewCutoffRetainsUnresolvedSourceEvents(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	snapshot := imp.BrokerSnapshot{SHA256: "source", Rows: []imp.BrokerSourceRow{
		{Identity: "before", Raw: json.RawMessage(`{"date":"2023-12-31T00:00:00Z","type":"FUTURE_TYPE"}`)},
		{Identity: "boundary", Raw: json.RawMessage(`{"date":"2024-01-01T00:00:00Z","type":"FUTURE_TYPE"}`)},
		{Identity: "after", Raw: json.RawMessage(`{"date":"2024-01-02T00:00:00Z","type":"FUTURE_TYPE"}`)},
	}}
	result, err := reviewTrades(snapshot, at, "11111111-1111-4111-8111-111111111111")
	if err != nil || result.PostingAllowed || len(result.Rows) != 1 || result.Rows[0].SourceIdentity != "after" || result.Rows[0].State != "requires_review" {
		t.Fatal(result, err)
	}
	if _, err = reviewTrades(snapshot, at, "bad"); err == nil {
		t.Fatal("invalid ledger accepted")
	}
}
