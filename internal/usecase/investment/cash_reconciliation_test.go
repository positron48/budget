package investment

import (
	"errors"
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

func TestCashReconciliationExactAndHistoryFence(t *testing.T) {
	fund := op("fund", "deposit", "9007199254740993.123456789012", 1)
	reconcile := op("reconcile", "cash_reconciliation", "0", 2)
	reconcile.Note = "Statement settled cash"
	reconcile.CashReconciliation = &d.InvestmentCashReconciliation{ExpectedSettled: fund.Amount, ObservedSettled: "9007199254740993"}
	later := op("fee", "fee", "1", 3)
	r, e := Replay(testLedger(), []d.InvestmentOperation{later, reconcile, fund}, day.Add(4*time.Hour))
	if e != nil || r.Snapshot.Cash[0].Settled != "9007199254740992" {
		t.Fatal(r, e)
	}
	if r.CashLegs[1].Amount != "-0.123456789012" || len(r.ProfitEvents) != 1 {
		t.Fatal("correction must not be profit", r)
	}
	before, e := Replay(testLedger(), []d.InvestmentOperation{later, reconcile, fund}, fund.OccurredAt)
	if e != nil || before.Snapshot.Cash[0].Settled != fund.Amount {
		t.Fatal(before, e)
	}
	fund.Amount = "9007199254740994.123456789012"
	_, e = Replay(testLedger(), []d.InvestmentOperation{fund, reconcile, later}, day.Add(4*time.Hour))
	var conflict *ReplayOperationError
	if !errors.As(e, &conflict) || conflict.OperationID != reconcile.ID {
		t.Fatal("old-history change must require reconciliation review", e)
	}
}

func TestCashReconciliationSettlementsSameTimestampAndZeroFact(t *testing.T) {
	fund := op("fund", "deposit", "100", 1)
	buy := op("buy", "buy", "30", 2)
	buy.Quantity = "1"
	buy.InstrumentKey = "stock"
	buy.SettlementAt = day.Add(4 * time.Hour)
	reconcile := op("reconcile", "cash_reconciliation", "0", 3)
	reconcile.OccurredAt = buy.SettlementAt
	reconcile.Note = "Broker settled balance"
	reconcile.CashReconciliation = &d.InvestmentCashReconciliation{ExpectedSettled: "70", ObservedSettled: "69"}
	r, e := Replay(testLedger(), []d.InvestmentOperation{reconcile, buy, fund}, buy.SettlementAt)
	if e != nil || r.Snapshot.Cash[0].Settled != "69" || r.Snapshot.Cash[0].Payable != "0" {
		t.Fatal(r, e)
	}
	reconcile.CashReconciliation = &d.InvestmentCashReconciliation{ExpectedSettled: "70", ObservedSettled: "70"}
	r, e = Replay(testLedger(), []d.InvestmentOperation{reconcile, buy, fund}, buy.SettlementAt)
	if e != nil || r.Snapshot.Cash[0].Settled != "70" || len(r.Snapshot.Issues) != 0 {
		t.Fatal(r, e)
	}
	found := false
	for _, leg := range r.CashLegs {
		if leg.OperationID == reconcile.ID && leg.Amount == "0" {
			found = true
		}
	}
	if !found {
		t.Fatal("zero observation must retain replay validation marker")
	}
	duplicate := reconcile
	duplicate.ID = "duplicate"
	duplicate.Sequence++
	if _, e = Replay(testLedger(), []d.InvestmentOperation{reconcile, duplicate, buy, fund}, buy.SettlementAt); e == nil {
		t.Fatal("ambiguous simultaneous observations accepted")
	}
	reconcile.CashReconciliation = &d.InvestmentCashReconciliation{ExpectedSettled: "100", ObservedSettled: "99"}
	reconcile.OccurredAt = day.Add(3 * time.Hour)
	r, e = Replay(testLedger(), []d.InvestmentOperation{reconcile, buy, fund}, reconcile.OccurredAt)
	if e != nil || r.Snapshot.Cash[0].Settled != "99" || r.Snapshot.Cash[0].Payable != "30" || r.Snapshot.Cash[0].Economic != "69" {
		t.Fatal("reconciliation must preserve unsettled obligations", r, e)
	}
}

func TestCashReconciliationValidation(t *testing.T) {
	base := op("r", "cash_reconciliation", "0", 1)
	base.Note = "Statement"
	base.CashReconciliation = &d.InvestmentCashReconciliation{ExpectedSettled: "1", ObservedSettled: "2"}
	if e := ValidateOperation(&base); e != nil || base.Amount != "1" {
		t.Fatal(base, e)
	}
	for _, change := range []func(*d.InvestmentOperation){func(o *d.InvestmentOperation) { o.Note = "" }, func(o *d.InvestmentOperation) { o.Kind = "deposit" }, func(o *d.InvestmentOperation) { o.Amount = "3" }, func(o *d.InvestmentOperation) { o.CashReconciliation = nil }, func(o *d.InvestmentOperation) {
		o.CashReconciliation = &d.InvestmentCashReconciliation{ExpectedSettled: "", ObservedSettled: "2"}
	}, func(o *d.InvestmentOperation) {
		o.CashReconciliation = &d.InvestmentCashReconciliation{ExpectedSettled: "1", ObservedSettled: "-2"}
	}} {
		o := base
		change(&o)
		if e := ValidateOperation(&o); e == nil {
			t.Fatal("invalid terms accepted", o)
		}
	}
}
