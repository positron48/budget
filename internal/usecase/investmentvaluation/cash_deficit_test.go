package investmentvaluation

import (
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

func TestImportedDeficitKeepsSignedValueButBlocksCompleteNAV(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	s := d.InvestmentSnapshot{Ledger: d.InvestmentLedger{ID: "ledger", TenantID: "tenant", Version: 1, StartedAt: at}, AsOf: at, Cash: []d.InvestmentCash{{Currency: "USD", Settled: "-10", Receivable: "0", Payable: "0"}}, Issues: []string{"imported_cash_deficit_reviewed", "negative_imported_cash", "imported_cash_history_incomplete"}}
	got, e := Value("tenant", []d.InvestmentSnapshot{s}, nil, nil, "USD", "market")
	if e != nil || got.Complete || got.KnownValue.RatString() != "-10" || len(got.Issues) != 2 || len(got.Warnings) != 1 {
		t.Fatal(got, e)
	}
	// Filling cash later does not repair historical coverage; backdated repair
	// removes the replay issue and restores ordinary valuation completeness.
	s.Cash[0].Settled = "0"
	s.Issues = []string{"imported_cash_deficit_reviewed", "imported_cash_history_incomplete"}
	got, e = Value("tenant", []d.InvestmentSnapshot{s}, nil, nil, "USD", "market")
	if e != nil || got.Complete {
		t.Fatal(got, e)
	}
	s.Issues = []string{"imported_cash_deficit_reviewed"}
	got, e = Value("tenant", []d.InvestmentSnapshot{s}, nil, nil, "USD", "market")
	if e != nil || !got.Complete || len(got.Warnings) != 1 {
		t.Fatal(got, e)
	}
}
