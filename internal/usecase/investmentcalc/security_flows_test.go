package investmentcalc

import (
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"testing"
	"time"
)

func securityFlowPair() []d.InvestmentOperation {
	at := time.Date(2024, 1, 1, 23, 0, 0, 0, time.UTC)
	a := d.InvestmentOperation{ID: "out", TenantID: "tenant", LedgerID: "a", CounterpartyLedgerID: "b", TransferID: "move", Kind: "security_transfer_out", InstrumentKey: "S", Currency: "RUB", Quantity: "2", Amount: "0", Status: "posted", OccurredAt: at}
	b := a
	b.ID = "in"
	b.Kind = "security_transfer_in"
	b.LedgerID = "b"
	b.CounterpartyLedgerID = "a"
	return []d.InvestmentOperation{a, b}
}
func TestSecurityFlowsUseScopeAndTransferDayMarketValue(t *testing.T) {
	pair := securityFlowPair()
	calls := 0
	value := func(op d.InvestmentOperation) (SecurityFlowValue, error) {
		calls++
		return SecurityFlowValue{Value: big.NewRat(100, 3), Currency: "USD", Date: "2024-01-02", SourceID: "reviewed-close-and-accrued"}, nil
	}
	internal, e := ExternalFlowsWithSecurities([]string{"a", "b"}, pair, value)
	if e != nil || len(internal) != 0 || calls != 0 {
		t.Fatal(internal, e, calls)
	}
	outbound, e := ExternalFlowsWithSecurities([]string{"a"}, pair[:1], value)
	if e != nil || outbound[0].Amount.RatString() != "-100/3" || outbound[0].Currency != "USD" || outbound[0].ValueSource != "reviewed-close-and-accrued" {
		t.Fatal(outbound, e)
	}
	inbound, e := ExternalFlowsWithSecurities([]string{"b"}, pair[1:], value)
	if e != nil || inbound[0].Amount.RatString() != "100/3" {
		t.Fatal(inbound, e)
	}
	unknown, e := ExternalFlows([]string{"a"}, pair[:1])
	if e != nil || unknown[0].Amount != nil || unknown[0].Reason != "security_transfer_market_value_missing" {
		t.Fatal(unknown, e)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	converted, e := ConvertExternalFlows(unknown, "RUB", loc, func(string) (*FXResolver, error) {
		t.Fatal("unknown market value must not resolve FX")
		return nil, nil
	})
	if e != nil || converted[0].Value != nil {
		t.Fatal(converted, e)
	}
	daily, e := DailyExternalFlows("2024-01-01", "2024-01-02", converted)
	if e != nil || daily["2024-01-02"] != nil || daily["2024-01-01"].Sign() != 0 {
		t.Fatal(daily, e)
	}
	known, e := ConvertExternalFlows(outbound, "RUB", loc, func(date string) (*FXResolver, error) {
		if date != "2024-01-02" {
			t.Fatal(date)
		}
		at := pair[0].OccurredAt
		return NewFXResolver([]FXFixing{{ID: "fixing", Revision: 1, From: "USD", To: "RUB", Rate: "3", Policy: "market", At: at, AvailableAt: at, ValidUntil: at.Add(time.Hour)}}, "market", at)
	})
	if e != nil || known[0].Value.RatString() != "-100" || known[0].FX[0].ID != "fixing" {
		t.Fatal(known, e)
	}
	for _, bad := range []SecurityFlowValue{{Value: big.NewRat(-1, 1), Currency: "USD", Date: "2024-01-02", SourceID: "source"}, {Value: big.NewRat(1, 1), Currency: "USD", Date: "2024-01-01", SourceID: "source"}, {Value: big.NewRat(1, 1), Currency: "USD", Date: "2024-01-02"}} {
		if _, e = ExternalFlowsWithSecurities([]string{"a"}, pair[:1], func(d.InvestmentOperation) (SecurityFlowValue, error) { return bad, nil }); e == nil {
			t.Fatal("invalid value accepted", bad)
		}
	}
	pair[1].Quantity = "1"
	if _, e = ExternalFlows([]string{"a", "b"}, pair); e == nil {
		t.Fatal("mismatched pair accepted")
	}
	pair = securityFlowPair()
	pair[1].Kind = "transfer_in"
	pair[1].Amount = "1"
	pair[1].InstrumentKey = ""
	if _, e = ExternalFlows([]string{"a", "b"}, pair); e == nil {
		t.Fatal("mixed pair accepted")
	}
	if _, e = ExternalFlows([]string{"a", "b"}, securityFlowPair()[:1]); e == nil {
		t.Fatal("missing selected leg accepted")
	}
}

func TestSecurityTransferIsNeutralForConsolidatedAndSelectedAccountPerformance(t *testing.T) {
	pair := securityFlowPair()
	loc, _ := time.LoadLocation("Europe/Moscow")
	for _, tc := range []struct {
		ids               []string
		ops               []d.InvestmentOperation
		initial, terminal *big.Rat
	}{
		{[]string{"a", "b"}, pair, big.NewRat(150, 1), big.NewRat(150, 1)},
		{[]string{"a"}, pair[:1], big.NewRat(100, 1), big.NewRat(200, 3)},
		{[]string{"b"}, pair[1:], big.NewRat(50, 1), big.NewRat(250, 3)},
	} {
		flows, e := ExternalFlowsWithSecurities(tc.ids, tc.ops, func(d.InvestmentOperation) (SecurityFlowValue, error) {
			return SecurityFlowValue{Value: big.NewRat(100, 3), Currency: "USD", Date: "2024-01-02", SourceID: "reviewed-market-value"}, nil
		})
		if e != nil {
			t.Fatal(e)
		}
		converted, e := ConvertExternalFlows(flows, "USD", loc, func(string) (*FXResolver, error) { t.Fatal("identity conversion needs no FX"); return nil, nil })
		if e != nil {
			t.Fatal(e)
		}
		daily, e := DailyExternalFlows("2024-01-01", "2024-01-02", converted)
		if e != nil {
			t.Fatal(e)
		}
		p, e := CalculatePeriod(PeriodPerformanceInput{FlowCoverage: true, Days: []PerformanceDay{{Date: "2024-01-01", NAV: tc.initial, ExternalFlow: daily["2024-01-01"], Complete: true}, {Date: "2024-01-02", NAV: tc.terminal, ExternalFlow: daily["2024-01-02"], Complete: true}}})
		if e != nil || p.Profit == nil || p.Profit.Sign() != 0 || p.TWR.Return == nil || p.TWR.Return.Sign() != 0 {
			t.Fatal(tc.ids, p, e)
		}
	}
	unknown, e := ExternalFlows([]string{"a"}, pair[:1])
	if e != nil {
		t.Fatal(e)
	}
	converted, e := ConvertExternalFlows(unknown, "USD", loc, func(string) (*FXResolver, error) { return nil, nil })
	if e != nil {
		t.Fatal(e)
	}
	daily, e := DailyExternalFlows("2024-01-01", "2024-01-02", converted)
	if e != nil {
		t.Fatal(e)
	}
	p, e := CalculatePeriod(PeriodPerformanceInput{FlowCoverage: true, Days: []PerformanceDay{{Date: "2024-01-01", NAV: big.NewRat(100, 1), ExternalFlow: big.NewRat(0, 1), Complete: true}, {Date: "2024-01-02", NAV: big.NewRat(200, 3), ExternalFlow: daily["2024-01-02"], Complete: true}}})
	if e != nil || p.Profit != nil || p.TWR.Return != nil || p.XIRR.Rate != nil || p.Reason != "external_flow_unknown" {
		t.Fatal(p, e)
	}
}
