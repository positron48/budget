package investmentcalc

import (
	"math/big"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

func TestTrackerFlowsTradesIncomeAndFeesWithoutFunding(t *testing.T) {
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	makeOp := func(id, kind, amount string) d.InvestmentOperation {
		return d.InvestmentOperation{ID: id, LedgerID: "L", Kind: kind, Status: "posted", Currency: "USD", Amount: amount, OccurredAt: day}
	}
	buy := makeOp("buy", "buy", "100")
	buy.Accrued = "5"
	buy.Fee = "2"
	sell := makeOp("sell", "sell", "70")
	sell.Accrued = "3"
	sell.Fee = "1"
	sell.FeeCurrency = "EUR"
	div := makeOp("div", "dividend", "10")
	void := makeOp("void", "sell", "999")
	void.Status = "voided"
	ops := []d.InvestmentOperation{buy, sell, div, void, makeOp("deposit", "deposit", "100000"), makeOp("withdrawal", "withdrawal", "50000"), makeOp("split", "split", "0")}
	flows, e := TrackerFlows([]string{"L"}, ops, nil)
	if e != nil {
		t.Fatal(e)
	}
	totals := map[string]*big.Rat{}
	for _, f := range flows {
		if totals[f.Currency] == nil {
			totals[f.Currency] = new(big.Rat)
		}
		totals[f.Currency].Add(totals[f.Currency], f.Amount)
	}
	if totals["USD"].RatString() != "24" || totals["EUR"].RatString() != "1" {
		t.Fatal(totals)
	}
	// Same economic cash whether the statement's actual settlement contains
	// expenses or reports them independently; never add the fee twice.
	for _, included := range []bool{true, false} {
		op := makeOp("trade", "sell", "70")
		actual := "70"
		if included {
			actual = "68"
		}
		op.TradeFinancials = &d.InvestmentTradeFinancials{ActualSettlement: actual, ActualSettlementIncludesExpenses: included, Principal: d.InvestmentTradeMoney{Currency: "USD", At: day}, Fee: d.InvestmentTradeMoney{Amount: "2", Currency: "USD", At: day}}
		flows, e := TrackerFlows([]string{"L"}, []d.InvestmentOperation{op}, nil)
		if e != nil {
			t.Fatal(e)
		}
		sum := new(big.Rat)
		for _, f := range flows {
			sum.Add(sum, f.Amount)
		}
		if sum.RatString() != "-68" {
			t.Fatal(included, sum)
		}
	}
}

func TestTrackerFlowsTransferScopeAndUnknownOpening(t *testing.T) {
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	out := d.InvestmentOperation{ID: "out", LedgerID: "A", Kind: "security_transfer_out", Status: "posted", InstrumentKey: "S", Currency: "USD", Quantity: "2", TransferID: "pair", CounterpartyLedgerID: "B", OccurredAt: day}
	in := out
	in.ID, in.LedgerID, in.Kind, in.CounterpartyLedgerID = "in", "B", "security_transfer_in", "A"
	flows, e := TrackerFlows([]string{"A", "B"}, []d.InvestmentOperation{out, in}, nil)
	if e != nil || len(flows) != 0 {
		t.Fatal(flows, e)
	}
	flows, e = TrackerFlows([]string{"A"}, []d.InvestmentOperation{out}, nil)
	if e != nil || len(flows) != 1 || flows[0].Amount != nil {
		t.Fatal(flows, e)
	}
	flows, e = TrackerFlows([]string{"A"}, []d.InvestmentOperation{out}, func(d.InvestmentOperation) (SecurityFlowValue, error) {
		return SecurityFlowValue{Value: big.NewRat(20, 1), Currency: "USD", Date: "2024-01-01", SourceID: "reviewed"}, nil
	})
	if e != nil || flows[0].Amount.RatString() != "-20" {
		t.Fatal(flows, e)
	}
	in.Kind = "opening_position"
	in.CounterpartyLedgerID, in.TransferID = "", ""
	flows, e = TrackerFlows([]string{"B"}, []d.InvestmentOperation{in}, nil)
	if e != nil || flows[0].Amount != nil || flows[0].Reason != "history_reset" {
		t.Fatal(flows, e)
	}
}
