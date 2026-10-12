package investment

import (
	"encoding/json"
	d "github.com/positron48/budget/internal/domain"
	"strings"
	"testing"
)

func TestMutationComparisonExactNativeDeltas(t *testing.T) {
	before := d.InvestmentSnapshot{Cash: []d.InvestmentCash{{Currency: "USD", Settled: "9007199254740993.123456789012", Receivable: "0", Payable: "0", Economic: "9007199254740993.123456789012"}}, Positions: []d.InvestmentPosition{{InstrumentKey: "stock", Currency: "USD", Quantity: "1", CleanCost: "10"}}}
	after := d.InvestmentSnapshot{Cash: []d.InvestmentCash{{Currency: "USD", Settled: "9007199254740993.123456789011", Receivable: "0", Payable: "0", Economic: "9007199254740993.123456789011"}, {Currency: "EUR", Settled: "1", Receivable: "0", Payable: "0", Economic: "1"}}, Positions: []d.InvestmentPosition{{InstrumentKey: "stock", Currency: "EUR", Quantity: "2", CleanCost: "20"}}}
	c := CompleteMutationComparison(MutationComparison{Before: before, After: after})
	if len(c.CashChange) != 2 || c.CashChange[1].Settled != "-0.000000000001" || c.CashChange[0].Settled != "1" || c.PositionChange[0].Quantity != "1" || c.PositionChange[0].CleanCost != "" {
		t.Fatal(c)
	}
	after.Positions[0].Currency = "USD"
	c = CompleteMutationComparison(MutationComparison{Before: before, After: after})
	if c.PositionChange[0].CleanCost != "10" {
		t.Fatal(c)
	}
	after.Positions[0].CleanCost = ""
	c = CompleteMutationComparison(MutationComparison{Before: before, After: after})
	if c.PositionChange[0].CleanCost != "" {
		t.Fatal("unknown cost became zero")
	}
}
func TestEmptyPreviewHashPreservesLegacyMutationEncoding(t *testing.T) {
	raw, e := json.Marshal(Mutation{})
	if e != nil || strings.Contains(string(raw), "PreviewHash") {
		t.Fatal("empty preview changes existing request hashes", e, string(raw))
	}
}
