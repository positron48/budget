package main

import (
	"encoding/json"
	"github.com/positron48/budget/internal/adapter/tinvest"
	"testing"
)

func TestHistoryEvidenceRequiresTerminalChainedPage(t *testing.T) {
	var h HistoryEvidence
	first := tinvest.OperationsPage{HasNext: true, NextCursor: "next", Items: []json.RawMessage{json.RawMessage(`{"id":"one"}`)}}
	if err := h.append("", first); err != nil || h.Complete {
		t.Fatal(h, err)
	}
	if err := h.append("wrong", tinvest.OperationsPage{}); err == nil || len(h.Pages) != 1 {
		t.Fatal(h, err)
	}
	if err := h.append("next", tinvest.OperationsPage{Items: []json.RawMessage{}}); err != nil || !h.Complete || len(h.Pages) != 2 || h.Pages[0].Count != 1 {
		t.Fatal(h, err)
	}
	if err := h.append("", tinvest.OperationsPage{}); err == nil {
		t.Fatal("page after completion accepted")
	}
	var changed HistoryEvidence
	first.Items = []json.RawMessage{json.RawMessage(`{"id":"two"}`)}
	_ = changed.append("", first)
	if changed.Pages[0].ItemsSHA256 == h.Pages[0].ItemsSHA256 {
		t.Fatal("hash does not bind content")
	}
}
func TestHistoryEvidenceRejectsCycle(t *testing.T) {
	var h HistoryEvidence
	for _, p := range []struct{ cursor, next string }{{"", "a"}, {"a", "b"}} {
		if err := h.append(p.cursor, tinvest.OperationsPage{HasNext: true, NextCursor: p.next}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.append("b", tinvest.OperationsPage{HasNext: true, NextCursor: "a"}); err == nil || h.Complete || len(h.Pages) != 2 {
		t.Fatal(h, err)
	}
}

func TestVerifyHistoryDetectsTamperedAndMissingPages(t *testing.T) {
	var s Snapshot
	s.Operations = []json.RawMessage{json.RawMessage(`{"id":"a"}`), json.RawMessage(`{"id":"b"}`)}
	_ = s.HistoryEvidence.append("", tinvest.OperationsPage{HasNext: true, NextCursor: "next", Items: s.Operations[:1]})
	_ = s.HistoryEvidence.append("next", tinvest.OperationsPage{Items: s.Operations[1:]})
	raw, _ := json.MarshalIndent(s, "", "  ")
	var loaded Snapshot
	_ = json.Unmarshal(raw, &loaded)
	if err := verifyHistory(loaded); err != nil {
		t.Fatal(err)
	}
	loaded.Operations[1] = json.RawMessage(`{"id":"changed"}`)
	if err := verifyHistory(loaded); err == nil {
		t.Fatal("changed operations accepted")
	}
	loaded = s
	loaded.Operations = loaded.Operations[:1]
	if err := verifyHistory(loaded); err == nil {
		t.Fatal("missing operations accepted")
	}
	loaded = s
	loaded.HistoryEvidence.Pages = loaded.HistoryEvidence.Pages[:1]
	if err := verifyHistory(loaded); err == nil {
		t.Fatal("missing last page accepted")
	}
	var legacy Snapshot
	if err := verifyHistory(legacy); err == nil {
		t.Fatal("legacy snapshot accepted as complete")
	}
}
