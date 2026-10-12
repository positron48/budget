package grpcadapter

import (
	inv "github.com/positron48/budget/internal/usecase/investment"
	"math/big"
	"testing"
)

func TestJournalAmountPreservesTwelveDecimals(t *testing.T) {
	for _, s := range []string{"9007199254740993.123456789012", "-0.000000000001", "0"} {
		v, ok := new(big.Rat).SetString(s)
		if !ok {
			t.Fatal(s)
		}
		p := protoPayoutAmount(v)
		if p.Decimal != s {
			t.Fatalf("rounded financial fact %s -> %s", s, p.Decimal)
		}
		n, _ := new(big.Int).SetString(p.Numerator, 10)
		d, _ := new(big.Int).SetString(p.Denominator, 10)
		if new(big.Rat).SetFrac(n, d).Cmp(v) != 0 {
			t.Fatal("fraction differs")
		}
	}
	if protoPayoutAmount(nil) != nil {
		t.Fatal("unknown converted to zero")
	}
}

func TestPayoutGrowthWireKeepsUndefinedAndExactPercent(t *testing.T) {
	g := inv.PayoutGrowth{Month: "2024-02", PreviousMonth: "2023-02", Currency: "USD", Status: "zero_baseline", IncomeNet: big.NewRat(1, 1), PreviousIncomeNet: big.NewRat(0, 1), Change: big.NewRat(1, 1)}
	p := protoPayoutGrowth(g)
	if p.PercentChange != nil || p.PreviousIncomeNet == nil || p.PreviousIncomeNet.Decimal != "0" {
		t.Fatal("undefined percent converted to zero", p)
	}
	g.Status = "available"
	g.PercentChange = big.NewRat(200, 7)
	p = protoPayoutGrowth(g)
	if p.PercentChange.Numerator != "200" || p.PercentChange.Denominator != "7" {
		t.Fatal("exact percent lost", p)
	}
}
