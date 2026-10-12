package investmentvaluation

import (
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"testing"
)

func TestDistributionContributionFairValueIsBalancedAndSeparate(t *testing.T) {
	op := d.InvestmentOperation{Kind: "distribution", LedgerID: "a", InstrumentKey: "parent", Distribution: &d.InvestmentDistribution{TargetInstrumentKey: "child", MarketValue: "40", MarketValueEvidence: "Reviewed total at event"}}
	for _, zero := range []bool{false, true} {
		b := NewContributionBuilder()
		b.Add("parent", "a", "opening_market", big.NewRat(-100, 1), "")
		b.Add("parent", "a", "closing_market", big.NewRat(70, 1), "")
		b.Add("child", "a", "closing_market", big.NewRat(45, 1), "")
		value := big.NewRat(40, 1)
		if zero {
			value.SetInt64(0)
			op.Distribution.MarketValue = "0"
		}
		b.ApplyDistribution(op, value, "")
		if value.Sign() < 0 {
			t.Fatal("mutated conversion")
		}
		out := b.Finish(big.NewRat(15, 1))
		if !out.Complete || out.Remainder.Known.Sign() != 0 {
			t.Fatal(out)
		}
		wantChild, wantParent := "5", "10"
		if zero {
			wantChild, wantParent = "45", "-30"
		}
		if out.Rows[0].Known.RatString() != wantChild || out.Rows[1].Known.RatString() != wantParent || out.Rows[0].Components[2].Known.Sign() != 0 || out.Rows[1].Components[2].Known.Sign() != 0 {
			t.Fatal(out.Rows)
		}
	}
}

func TestDistributionContributionMissingFXNeverFallsBackToFIFOCost(t *testing.T) {
	b := NewContributionBuilder()
	op := d.InvestmentOperation{Kind: "distribution", LedgerID: "a", InstrumentKey: "parent", Distribution: &d.InvestmentDistribution{TargetInstrumentKey: "child", MarketValue: "40", MarketValueEvidence: "Reviewed", CostNumerator: "1", CostDenominator: "2"}}
	b.ApplyDistribution(op, nil, "fx_missing")
	out := b.Finish(big.NewRat(15, 1))
	if out.Complete || out.Remainder.Known != nil {
		t.Fatal(out)
	}
	for _, row := range out.Rows {
		if row.Complete || row.Share != nil || row.Components[3].Known != nil || row.Components[3].Reason != "fx_missing" {
			t.Fatal(row)
		}
	}
}
