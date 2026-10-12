package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

func TestLedgerClosureRequiresZeroInEveryBucket(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	s := d.InvestmentSnapshot{Cash: []d.InvestmentCash{{Currency: "USD", Settled: "0", Receivable: "0", Payable: "0", Economic: "0"}}, Positions: []d.InvestmentPosition{{Quantity: "0"}}}
	if e := CheckLedgerClosure(s, nil, at); e != nil {
		t.Fatal(e)
	}
	for _, c := range []d.InvestmentCash{{Settled: "0.000000000001", Receivable: "0", Payable: "0", Economic: "0.000000000001"}, {Settled: "-1", Receivable: "1", Payable: "0", Economic: "0"}, {Settled: "0", Receivable: "1", Payable: "1", Economic: "0"}} {
		bad := s
		bad.Cash = []d.InvestmentCash{c}
		if CheckLedgerClosure(bad, nil, at) == nil {
			t.Fatal("nonzero bucket accepted", c)
		}
	}
	bad := s
	bad.Positions = []d.InvestmentPosition{{Quantity: "0.0000000001"}}
	if CheckLedgerClosure(bad, nil, at) == nil {
		t.Fatal("small position accepted")
	}
	for _, op := range []d.InvestmentOperation{{Status: "posted", OccurredAt: at.Add(time.Hour), SettlementAt: at.Add(time.Hour)}, {Status: "posted", OccurredAt: at, SettlementAt: at.Add(time.Hour)}} {
		if CheckLedgerClosure(s, []d.InvestmentOperation{op}, at) == nil {
			t.Fatal("future accepted")
		}
	}
}
