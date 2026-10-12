package investment

import (
	"errors"
	d "github.com/positron48/budget/internal/domain"
	"slices"
	"testing"
	"time"
)

func importedDeficit(o d.InvestmentOperation) d.InvestmentOperation {
	o.ImportedCashDeficitReason = "Missing funding in original broker history; retain actual trade"
	o.Source = &d.InvestmentSource{ImportID: "11111111-1111-4111-8111-111111111111", Record: 1, ExpectedVersion: 1, ReviewReason: "Confirmed source"}
	return o
}
func TestImportedCashDeficitIsPerOperationAndTimestamp(t *testing.T) {
	buy := importedDeficit(op("buy", "buy", "100", 1))
	buy.InstrumentKey = "STOCK"
	buy.Quantity = "1"
	buy.SettlementAt = day.Add(48 * time.Hour)
	before, e := Replay(testLedger(), []d.InvestmentOperation{buy}, day.Add(24*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if c := before.Snapshot.Cash[0]; c.Settled != "0" || c.Payable != "100" || c.Economic != "-100" {
		t.Fatal(c)
	}
	after, e := Replay(testLedger(), []d.InvestmentOperation{buy}, day.Add(72*time.Hour))
	if e != nil || after.Snapshot.Cash[0].Settled != "-100" || !slices.Contains(after.Snapshot.Issues, "negative_imported_cash") {
		t.Fatal(after.Snapshot, e)
	}
	// Neither an existing deficit nor an earlier import exception enables a
	// subsequent ordinary debit. Same-time manual debits are also checked.
	for _, at := range []time.Time{buy.OccurredAt, buy.SettlementAt, buy.SettlementAt.Add(time.Hour)} {
		fee := op("fee", "fee", "1", 2)
		fee.OccurredAt = at
		if _, e = Replay(testLedger(), []d.InvestmentOperation{buy, fee}, day.Add(96*time.Hour)); !errors.Is(e, d.ErrInvestmentPrecondition) {
			t.Fatal(at, e)
		}
	}
	// An unrelated timestamp cannot erase the historical deficiency warning.
	fund := op("fund", "deposit", "100", 2)
	fund.OccurredAt = buy.SettlementAt.Add(time.Hour)
	got, e := Replay(testLedger(), []d.InvestmentOperation{buy, fund}, day.Add(96*time.Hour))
	if e != nil || got.Snapshot.Cash[0].Settled != "0" || slices.Contains(got.Snapshot.Issues, "negative_imported_cash") || !slices.Contains(got.Snapshot.Issues, "imported_cash_history_incomplete") {
		t.Fatal(got.Snapshot, e)
	}
	fee := op("fee", "fee", "1", 3)
	fee.OccurredAt = fund.OccurredAt.Add(time.Hour)
	if _, e = Replay(testLedger(), []d.InvestmentOperation{buy, fund, fee}, day.Add(96*time.Hour)); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("reused paid-down deficit", e)
	}
	// Repair the missing funding at its real historical date.
	fund.OccurredAt = day
	got, e = Replay(testLedger(), []d.InvestmentOperation{buy, fund}, day.Add(96*time.Hour))
	if e != nil || slices.Contains(got.Snapshot.Issues, "imported_cash_history_incomplete") {
		t.Fatal(got.Snapshot, e)
	}
}

func TestImportedCashDeficitRequiresSourceAndDoesNotPermitShorts(t *testing.T) {
	withdrawal := importedDeficit(op("withdraw", "withdrawal", "10", 1))
	invalid := []d.InvestmentOperation{withdrawal, withdrawal, withdrawal}
	invalid[0].Source = nil
	copySource := *withdrawal.Source
	copySource.ImportID = "fake"
	invalid[1].Source = &copySource
	invalid[2].ImportedCashDeficitReason = "   "
	for _, o := range invalid {
		if _, e := Replay(testLedger(), []d.InvestmentOperation{o}, day.Add(24*time.Hour)); !errors.Is(e, d.ErrInvestmentInvalid) {
			t.Fatal(e)
		}
	}
	sell := importedDeficit(op("sell", "sell", "10", 1))
	sell.Quantity = "1"
	sell.InstrumentKey = "STOCK"
	if _, e := Replay(testLedger(), []d.InvestmentOperation{sell}, day.Add(24*time.Hour)); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("short allowed", e)
	}
	// Client-provided operation.Source is always discarded; it cannot authorize
	// an exception in an ordinary create command.
	_, e := NormalizeMutation("t", "u", Mutation{Action: "create", RequestKey: "x", ExpectedLedgerVersion: 1, Operation: withdrawal}, day.Add(24*time.Hour))
	if !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal(e)
	}
}

func TestImportedCashDeficitDoesNotCrossCurrencyOrReuseReceivable(t *testing.T) {
	usd := importedDeficit(op("usd", "withdrawal", "10", 1))
	rub := op("rub", "withdrawal", "1", 2)
	rub.Currency = "RUB"
	rub.OccurredAt = usd.OccurredAt
	if _, e := Replay(testLedger(), []d.InvestmentOperation{usd, rub}, day.Add(24*time.Hour)); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(e)
	}
	// Sale settlement reverses a receivable and receives cash: net economic
	// change is zero, so it must not authorize an unrelated withdrawal.
	position := op("position", "opening_position", "10", 1)
	position.OccurredAt = day
	position.InstrumentKey = "STOCK"
	position.Quantity = "1"
	position.OpeningCostKnown = true
	position.Note = "Confirmed opening position"
	sell := importedDeficit(op("sell", "sell", "10", 2))
	sell.InstrumentKey = "STOCK"
	sell.Quantity = "1"
	sell.SettlementAt = day.Add(48 * time.Hour)
	spend := op("spend", "withdrawal", "11", 3)
	spend.OccurredAt = sell.SettlementAt
	if _, e := Replay(testLedger(), []d.InvestmentOperation{position, sell, spend}, day.Add(72*time.Hour)); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(e)
	}
}
