package investmentvaluation

import (
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

func TestPortfolioCoverageGapKeepsOtherCash(t *testing.T) {
	at := time.Date(2024, 1, 1, 20, 59, 59, 0, time.UTC)
	snapshots := []d.InvestmentSnapshot{
		{Ledger: d.InvestmentLedger{ID: "old", TenantID: "tenant", Version: 1, StartedAt: at.Add(-24 * time.Hour)}, AsOf: at, Cash: []d.InvestmentCash{{Currency: "RUB", Settled: "100", Receivable: "0", Payable: "0"}}},
		{Ledger: d.InvestmentLedger{ID: "later", TenantID: "tenant", Version: 1, StartedAt: at.Add(24 * time.Hour)}, AsOf: at},
	}
	result, e := Value("tenant", snapshots, nil, nil, "RUB", "market")
	if e != nil || result.Complete || result.KnownValue.RatString() != "100" || len(result.Issues) != 1 || result.Issues[0].Code != "ledger_history_missing" || result.LedgerVersions["later"] != 1 {
		t.Fatal(result, e)
	}
	snapshots[1].Cash = snapshots[0].Cash
	if _, e = Value("tenant", snapshots, nil, nil, "RUB", "market"); e == nil {
		t.Fatal("unverified pre-opening money accepted")
	}
}

func TestNormalizePortfolioRejectsDuplicateAccounts(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	input := PortfolioInput{Name: " Portfolio ", LedgerIDs: []string{id}, Currency: "RUB", FXPolicyKey: "market", ConventionKey: "reviewed", From: "2024-01-01", To: "2024-01-02", RequestKey: "create"}
	got, e := NormalizePortfolio(input)
	if e != nil || got.Name != "Portfolio" {
		t.Fatal(got, e)
	}
	input.LedgerIDs = append(input.LedgerIDs, id)
	if _, e = NormalizePortfolio(input); e == nil {
		t.Fatal("duplicate account accepted")
	}
}

func TestLastCompletedMoscowDate(t *testing.T) {
	for _, tc := range []struct{ instant, want string }{
		{"2024-01-02T20:59:59Z", "2024-01-01"},
		{"2024-01-02T21:00:00Z", "2024-01-02"},
		{"2024-02-29T21:00:00Z", "2024-02-29"},
		{"2010-07-02T19:59:59Z", "2010-07-01"},
		{"2010-07-02T20:00:00Z", "2010-07-02"},
	} {
		at, e := time.Parse(time.RFC3339, tc.instant)
		if e != nil {
			t.Fatal(e)
		}
		if got := LastCompletedDate(at); got != tc.want {
			t.Fatal(tc.instant, got, tc.want)
		}
	}
}

func TestTrackerSelectionContainsOnlyAccountReferences(t *testing.T) {
	p := PortfolioInput{TrackerSelection: true, Name: " Brokers ", LedgerIDs: []string{"22222222-2222-4222-8222-222222222222", "11111111-1111-4111-8111-111111111111"}, RequestKey: "selection"}
	n, err := NormalizePortfolio(p)
	if err != nil || n.Name != "Brokers" || n.LedgerIDs[0] != p.LedgerIDs[1] {
		t.Fatal(n, err)
	}
	for _, mutate := range []func(*PortfolioInput){func(v *PortfolioInput) { v.Currency = "RUB" }, func(v *PortfolioInput) { v.FXPolicyKey = "market" }, func(v *PortfolioInput) { v.AutoExtend = true }, func(v *PortfolioInput) { v.From = "2024-01-01" }, func(v *PortfolioInput) { v.LedgerIDs = append(v.LedgerIDs, v.LedgerIDs[0]) }} {
		bad := p
		bad.LedgerIDs = append([]string{}, p.LedgerIDs...)
		mutate(&bad)
		if _, err := NormalizePortfolio(bad); err == nil {
			t.Fatal("selection accepted valuation configuration or duplicate", bad)
		}
	}
}
