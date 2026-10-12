package investmentcalc

import (
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

func TestReconciliationDifferenceNeverInventsContributionOrProfit(t *testing.T) {
	at := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	o := d.InvestmentOperation{ID: "r", LedgerID: "ledger", Status: "posted", Kind: "cash_reconciliation", Currency: "USD", OccurredAt: at, CashReconciliation: &d.InvestmentCashReconciliation{ExpectedSettled: "100", ObservedSettled: "99"}}
	for _, observed := range []string{"99", "101", "100"} {
		o.CashReconciliation.ObservedSettled = observed
		flows, e := ExternalFlows([]string{"ledger"}, []d.InvestmentOperation{o})
		if e != nil || len(flows) != 1 {
			t.Fatal(flows, e)
		}
		converted, e := ConvertExternalFlows(flows, "USD", time.UTC, func(string) (*FXResolver, error) { t.Fatal("same currency must not fetch FX"); return nil, nil })
		if e != nil {
			t.Fatal(e)
		}
		daily, e := DailyExternalFlows("2024-01-01", "2024-01-03", converted)
		if e != nil {
			t.Fatal(e)
		}
		days := []PerformanceDay{performanceDay("2024-01-01", "100", "0"), performanceDay("2024-01-02", observed, "0")}
		days[1].ExternalFlow = daily["2024-01-02"]
		result, e := CalculatePeriod(PeriodPerformanceInput{Days: days, FlowCoverage: true})
		if e != nil {
			t.Fatal(e)
		}
		if observed != "100" && (result.Profit != nil || result.XIRR.Rate != nil || result.TWR.Return != nil || result.Reason != "external_flow_unknown") {
			t.Fatal("reconciliation manufactured a return", result)
		}
		if observed == "100" {
			if daily["2024-01-02"] == nil || daily["2024-01-02"].Sign() != 0 {
				t.Fatal(daily)
			}
		} else if daily["2024-01-02"] != nil || flows[0].Reason != "cash_reconciliation_unclassified" {
			t.Fatal("unexplained difference was classified", daily, flows)
		}
	}
}
