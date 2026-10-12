package main

import (
	d "github.com/positron48/budget/internal/domain"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"reflect"
	"testing"
	"time"
)

func TestCandidateReplayKeepsUnresolvedAndNeverPublishesFailedSnapshot(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	good := d.InvestmentOperation{LedgerID: "good", Kind: "deposit", Currency: "RUB", Amount: "100", OccurredAt: at}
	bad := d.InvestmentOperation{LedgerID: "bad", Kind: "sell", InstrumentKey: "TEST", Currency: "RUB", Amount: "10", Quantity: "1", OccurredAt: at}
	report := imp.FinancialMappingReport{SourceSHA256: "source", Hash: "hash", Ready: 2, Unresolved: 1, Mapping: imp.FinancialMapping{LedgerByRecord: map[int]string{1: "good", 2: "bad", 3: "good"}}, Rows: []imp.FinancialCandidate{{Record: 1, Operation: &good}, {Record: 2, Operation: &bad}, {Record: 3, Issues: []string{"financial_rule_required"}}}}
	out := replayCandidates(report)
	if len(out.Ledgers) != 2 || out.Unresolved != 1 {
		t.Fatal(out)
	}
	if out.Ledgers[0].State != "replay_failed" || out.Ledgers[0].Snapshot != nil || out.Ledgers[0].Error == "" || out.Ledgers[0].FailedRecord != 2 || out.Ledgers[0].FailedInstrument != "TEST" {
		t.Fatal(out.Ledgers[0])
	}
	passed := out.Ledgers[1]
	if passed.State != "candidate_replay_passed" || passed.Snapshot.Cash[0].Economic != "100" || !reflect.DeepEqual(passed.UnresolvedRecords, []int{3}) {
		t.Fatal(passed)
	}
	if good.ID != "" || good.Status != "" {
		t.Fatal("input mutated", good)
	}
	if !reflect.DeepEqual(out, replayCandidates(report)) {
		t.Fatal("non-deterministic replay")
	}
}
func TestCandidateReplayIncludesDelayedSettlementAndEmptyAssignments(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	deposit := d.InvestmentOperation{LedgerID: "ledger", Kind: "deposit", Currency: "USD", Amount: "100", OccurredAt: at}
	buy := d.InvestmentOperation{LedgerID: "ledger", Kind: "buy", InstrumentKey: "TEST", Currency: "USD", Amount: "20", Quantity: "1", OccurredAt: at.Add(time.Hour), SettlementAt: at.Add(48 * time.Hour)}
	report := imp.FinancialMappingReport{Ready: 2, Unresolved: 1, Rows: []imp.FinancialCandidate{{Record: 1, Operation: &deposit}, {Record: 2, Operation: &buy}, {Record: 3}}}
	out := replayCandidates(report)
	if len(out.Ledgers) != 2 || out.Ledgers[0].State != "no_candidates" || out.Ledgers[0].Snapshot != nil {
		t.Fatal(out)
	}
	l := out.Ledgers[1]
	if !l.AsOf.Equal(buy.SettlementAt) || l.Snapshot.Cash[0].Settled != "80" || l.Snapshot.Cash[0].Payable != "0" {
		t.Fatal(l)
	}
}

func TestCandidateCashFailurePreservesRowsWithoutSnapshot(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	withdraw := d.InvestmentOperation{LedgerID: "ledger", Kind: "withdrawal", Currency: "USD", Amount: "10", OccurredAt: at}
	out := replayCandidates(imp.FinancialMappingReport{Ready: 1, Rows: []imp.FinancialCandidate{{Record: 7, Operation: &withdraw}}})
	l := out.Ledgers[0]
	if l.Snapshot != nil || l.CashFailure == nil || l.CashFailure.Balance.Economic != "-10" || !reflect.DeepEqual(l.CashFailure.Records, []int{7}) {
		t.Fatal(l)
	}
}
