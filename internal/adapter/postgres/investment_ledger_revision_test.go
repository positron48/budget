package postgres

import (
	"encoding/json"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

func TestLedgerRevisionPreservesInstantsWithHistoricalSecondOffset(t *testing.T) {
	at := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC).In(time.FixedZone("historical", 2*3600+30*60+17))
	before := d.InvestmentLedger{StartedAt: at, CreatedAt: at, PortfolioTracking: true}
	raw, e := json.Marshal(ledgerRevisionJSON(before))
	if e != nil {
		t.Fatal(e)
	}
	after, e := parseLedgerRevision(raw)
	if e != nil || !after.StartedAt.Equal(before.StartedAt) || !after.CreatedAt.Equal(before.CreatedAt) || !after.PortfolioTracking {
		t.Fatal("ledger audit changed timestamp or tracking mode", e)
	}
}
