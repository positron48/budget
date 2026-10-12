package investmentvaluation

import (
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"testing"
)

func TestInstrumentContributionExactEconomicsAndShares(t *testing.T) {
	b := NewContributionBuilder()
	b.Ensure("sold", "b", false)
	b.Add("sold", "b", "opening_market", big.NewRat(-10, 1), "")
	b.Add("sold", "b", "closing_market", new(big.Rat), "")
	b.Add("sold", "b", "cash_movements", big.NewRat(35, 3), "")
	b.Ensure("held", "a", true)
	b.Add("held", "a", "closing_market", big.NewRat(8, 1), "")
	b.Add("held", "a", "cash_movements", big.NewRat(-6, 1), "")
	out := b.Finish(big.NewRat(4, 1))
	if !out.Complete || len(out.Rows) != 2 || out.Rows[0].InstrumentKey != "held" || out.Rows[0].Closed || !out.Rows[1].Closed || out.Rows[1].Known.RatString() != "5/3" || out.Rows[1].Share.RatString() != "5/12" || out.Remainder.Known.RatString() != "1/3" {
		t.Fatal(out)
	}
	// Read-only finish neither accumulates totals twice nor mutates component facts.
	again := b.Finish(new(big.Rat))
	if !again.Complete || again.Rows[1].Share != nil || again.Rows[1].Known.RatString() != "5/3" {
		t.Fatal(again)
	}
	b.Add("sold", "a", "cash_movements", big.NewRat(1, 3), "")
	out = b.Finish(big.NewRat(4, 1))
	if out.Rows[1].Known.RatString() != "2" || len(out.Rows[1].LedgerIDs) != 2 || out.Rows[1].LedgerIDs[0] != "a" {
		t.Fatal(out)
	}
}
func TestInstrumentContributionUnknownsNeverCreateSharesOrRemainder(t *testing.T) {
	b := NewContributionBuilder()
	b.Add("unknown", "a", "opening_market", nil, "ledger_history_missing")
	b.Add("unknown", "a", "closing_market", big.NewRat(20, 1), "")
	b.Add("unknown", "a", "cash_movements", nil, "fx_missing")
	out := b.Finish(big.NewRat(100, 1))
	row := out.Rows[0]
	if out.Complete || out.Remainder.Known != nil || out.Remainder.Reason == "" || row.Complete || row.Known.RatString() != "20" || row.Share != nil || row.Components[0].Known != nil || row.Components[2].Reason != "fx_missing" {
		t.Fatal(out)
	}
	if b.Finish(nil).Complete {
		t.Fatal("unknown total")
	}
}

func TestContributionDistributionDoesNotInventProfitFromNoncashTransfer(t *testing.T) {
	b := NewContributionBuilder()
	b.Add("parent", "a", "opening_market", big.NewRat(-100, 1), "")
	b.Add("child", "a", "closing_market", big.NewRat(100, 1), "")
	b.ApplyDistribution(d.InvestmentOperation{Kind: "distribution", LedgerID: "a", InstrumentKey: "parent", Distribution: &d.InvestmentDistribution{TargetInstrumentKey: "child"}}, nil, "")
	out := b.Finish(new(big.Rat))
	if out.Complete || out.Remainder.Known != nil || len(out.Rows) != 2 {
		t.Fatal(out)
	}
	for _, row := range out.Rows {
		if row.Complete || row.Share != nil || row.Components[3].Reason != "noncash_allocation_required" {
			t.Fatal(row)
		}
	}
}

func TestContributionUnassignedTaxReviewSuppressesShares(t *testing.T) {
	b := NewContributionBuilder()
	b.Add("held", "a", "closing_market", big.NewRat(10, 1), "")
	b.RestrictOperation(d.InvestmentOperation{Kind: "tax", LedgerID: "a", TaxReviewRequired: true})
	out := b.Finish(big.NewRat(10, 1))
	if out.Complete || out.Rows[0].Share != nil || out.Remainder.Known != nil {
		t.Fatal(out)
	}
}
