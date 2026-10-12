package investmentcalc

import (
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"testing"
	"time"
)

func TestExternalFlowsPortfolioBoundary(t *testing.T) {
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	a := d.InvestmentOperation{ID: "a", LedgerID: "one", Kind: "transfer_out", Amount: "500", Currency: "RUB", OccurredAt: at, Status: "posted", TransferID: "transfer", CounterpartyLedgerID: "two"}
	b := a
	b.ID = "b"
	b.LedgerID = "two"
	b.CounterpartyLedgerID = "one"
	b.Kind = "transfer_in"
	flows, err := ExternalFlows([]string{"one", "two"}, []d.InvestmentOperation{a, b})
	if err != nil || len(flows) != 0 {
		t.Fatalf("internal: %v %v", flows, err)
	}
	flows, err = ExternalFlows([]string{"one"}, []d.InvestmentOperation{a})
	if err != nil || len(flows) != 1 || flows[0].Amount.Cmp(big.NewRat(-500, 1)) != 0 {
		t.Fatalf("withdrawal: %v %v", flows, err)
	}
	flows, err = ExternalFlows([]string{"two"}, []d.InvestmentOperation{b})
	if err != nil || len(flows) != 1 || flows[0].Amount.Cmp(big.NewRat(500, 1)) != 0 {
		t.Fatalf("contribution: %v %v", flows, err)
	}
	for _, mutate := range []func(*d.InvestmentOperation){func(o *d.InvestmentOperation) { o.Status = "voided" }, func(o *d.InvestmentOperation) { o.Amount = "499" }, func(o *d.InvestmentOperation) { o.Currency = "USD" }, func(o *d.InvestmentOperation) { o.OccurredAt = o.OccurredAt.Add(time.Hour) }, func(o *d.InvestmentOperation) { o.Kind = "transfer_out" }} {
		changed := b
		mutate(&changed)
		if _, err := ExternalFlows([]string{"one", "two"}, []d.InvestmentOperation{a, changed}); err == nil {
			t.Fatal("accepted inconsistent pair")
		}
	}
}

func TestExternalFlowsExcludeInvestmentResults(t *testing.T) {
	operations := []d.InvestmentOperation{}
	for _, kind := range []string{"opening_cash", "opening_position", "buy", "sell", "dividend", "coupon", "fee", "tax", "tax_refund", "split", "amortization", "redemption", "fx", "cash_interest", "repo_income", "distribution"} {
		operations = append(operations, d.InvestmentOperation{ID: kind, Kind: kind, LedgerID: "one", Status: "posted", OccurredAt: time.Now()})
	}
	flows, err := ExternalFlows([]string{"one"}, operations)
	if err != nil || len(flows) != 0 {
		t.Fatalf("results are not contributions: %v %v", flows, err)
	}
	operations[0].Kind = "unknown"
	if _, err = ExternalFlows([]string{"one"}, operations); err == nil {
		t.Fatal("unknown operation silently ignored")
	}
}

func TestConvertExternalFlowsHistoricalDates(t *testing.T) {
	location := time.FixedZone("Moscow", 3*3600)
	at := time.Date(2026, 1, 1, 21, 0, 0, 0, time.UTC)
	flows := []ExternalFlow{{OperationID: "first", Currency: "USD", At: at, Amount: big.NewRat(10, 1)}, {OperationID: "second", Currency: "USD", At: at.Add(24 * time.Hour), Amount: big.NewRat(-10, 1)}, {OperationID: "third", Currency: "EUR", At: at, Amount: big.NewRat(5, 1)}}
	calls := []string{}
	converted, err := ConvertExternalFlows(flows, "RUB", location, func(date string) (*FXResolver, error) {
		calls = append(calls, date)
		day, _ := time.Parse(time.DateOnly, date)
		rate := "90"
		if date == "2026-01-03" {
			rate = "100"
		}
		return NewFXResolver([]FXFixing{{ID: date, Revision: 1, From: "USD", To: "RUB", Rate: rate, Policy: "market", At: day, AvailableAt: day, ValidUntil: day.Add(24 * time.Hour)}}, "market", day)
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls[0] != "2026-01-02" || calls[1] != "2026-01-03" || converted[0].Value.Cmp(big.NewRat(900, 1)) != 0 || converted[1].Value.Cmp(big.NewRat(-1000, 1)) != 0 || converted[2].Value != nil || len(converted[0].FX) != 1 {
		t.Fatalf("wrong conversion: %+v", converted)
	}
	converted[0].Amount.SetInt64(0)
	if flows[0].Amount.Sign() == 0 {
		t.Fatal("mutated input")
	}
}

func TestDailyExternalFlowsUnknownAndZeroDays(t *testing.T) {
	flows := []ConvertedFlow{
		{ExternalFlow: ExternalFlow{OperationID: "one"}, Date: "2026-01-02", Value: big.NewRat(10, 1)},
		{ExternalFlow: ExternalFlow{OperationID: "two"}, Date: "2026-01-02"},
		{ExternalFlow: ExternalFlow{OperationID: "three"}, Date: "2026-01-02", Value: big.NewRat(-10, 1)},
	}
	totals, err := DailyExternalFlows("2026-01-01", "2026-01-03", flows)
	if err != nil || len(totals) != 3 || totals["2026-01-01"].Sign() != 0 || totals["2026-01-02"] != nil || totals["2026-01-03"].Sign() != 0 {
		t.Fatalf("daily completeness: %v %v", totals, err)
	}
	flows[1].Value = big.NewRat(5, 1)
	totals, err = DailyExternalFlows("2026-01-01", "2026-01-03", flows)
	if err != nil || totals["2026-01-02"].Cmp(big.NewRat(5, 1)) != 0 {
		t.Fatal("wrong exact total")
	}
	if _, err = DailyExternalFlows("2026-01-03", "2026-01-03", flows); err == nil {
		t.Fatal("silently dropped out-of-range payment")
	}
	if _, err = DailyExternalFlows("2026-01-01", "2026-01-03", append(flows, flows[0])); err == nil {
		t.Fatal("duplicate payment")
	}
}
