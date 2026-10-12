package investment

import (
	"errors"
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"testing"
	"time"
)

func TestOperationsPageSourcesFiltersStableOrderAndCash(t *testing.T) {
	ledgers, ops, _, now := payoutFixture()
	q := OperationsQuery{FromDate: "2024-02-01", ToDate: "2024-02-29", Limit: 1}
	// Same timestamps and sequence must still produce deterministic ID ordering.
	for i := range ops {
		ops[i].OccurredAt = time.Date(2024, 1, 31, 22, 0, 0, 0, time.UTC)
	}
	ops[0].Source = &d.InvestmentSource{ImportID: payoutOtherID, Record: 2, ExpectedVersion: 1, ReviewReason: "Reviewed"}
	linked := map[string]int64{"dividend": 1}
	out, e := BuildOperationsPage("tenant", ledgers, ops, linked, q)
	if e != nil {
		t.Fatal(e)
	}
	if out.TotalCount != int64(len(ops)) || len(out.Entries) != 1 || out.NextCursor == "" {
		t.Fatal(out)
	}
	seen := []string{}
	for {
		for _, r := range out.Entries {
			seen = append(seen, r.Operation.ID)
			if r.Operation.Status == "voided" && len(r.Cash) != 0 {
				t.Fatal("voided cash effect")
			}
		}
		if out.NextCursor == "" {
			break
		}
		q.Cursor = out.NextCursor
		out, e = BuildOperationsPage("tenant", ledgers, ops, linked, q)
		if e != nil {
			t.Fatal(e)
		}
	}
	for i := 1; i < len(seen); i++ {
		if seen[i-1] >= seen[i] {
			t.Fatal("unstable order", seen)
		}
	}
	q.Cursor = ""
	q.Origin = "import"
	out, e = BuildOperationsPage("tenant", ledgers, ops, linked, q)
	if e != nil || out.TotalCount != 1 || len(out.Entries[0].Origins) != 2 {
		t.Fatal("multiple provenance", out, e)
	}
	q.Origin = "broker"
	q.ImportID = payoutOtherID
	q.InstrumentKey = "STOCK"
	q.Status = "posted"
	out, e = BuildOperationsPage("tenant", ledgers, ops, linked, q)
	if e != nil || out.TotalCount != 1 {
		t.Fatal("source filters", e, out)
	}
	q.Origin = "manual"
	out, e = BuildOperationsPage("tenant", ledgers, ops, linked, q)
	if e != nil || out.TotalCount != 0 {
		t.Fatal("broker/import misclassified manual", e)
	}
	_ = now
}
func TestOperationsQueryFencesBackdatesAndEvidenceLinks(t *testing.T) {
	ledgers, ops, _, _ := payoutFixture()
	q := OperationsQuery{FromDate: "2024-01-01", ToDate: "2025-12-31", Limit: 1}
	out, e := BuildOperationsPage("tenant", ledgers, ops, nil, q)
	if e != nil {
		t.Fatal(e)
	}
	q.Cursor = out.NextCursor
	if _, e = BuildOperationsPage("tenant", ledgers, ops, map[string]int64{ops[0].ID: 1}, q); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("source link without ledger change accepted", e)
	}
	ops[0].Version++
	if _, e = BuildOperationsPage("tenant", ledgers, ops, nil, q); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("amendment accepted", e)
	}
	ops[0].Version--
	ledgers[0].Version++
	if _, e = BuildOperationsPage("tenant", ledgers, ops, nil, q); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("backdate accepted", e)
	}
	ledgers[0].Version--
	q.Cursor = ""
	q.LedgerIDs = []string{payoutLedgerID}
	selectedPage, err := BuildOperationsPage("tenant", ledgers, ops, nil, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Cursor = selectedPage.NextCursor
	ledgers[1].Version++
	if _, err = BuildOperationsPage("tenant", ledgers, ops, nil, q); err != nil {
		t.Fatal("unselected ledger invalidates cursor", err)
	}
	ledgers[1].Version--
	q.Cursor = ""
	q.LedgerIDs = []string{"00000000-0000-0000-0000-000000000003"}
	if _, e = BuildOperationsPage("tenant", ledgers, ops, nil, q); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign ledger", e)
	}
	for _, bad := range []OperationsQuery{{FromDate: "2024-02-30", ToDate: "2024-12-31"}, {FromDate: "2025-01-01", ToDate: "2024-01-01"}, {FromDate: "2024-01-01", ToDate: "2025-01-01", Kind: "unknown"}, {FromDate: "2024-01-01", ToDate: "2025-01-01", Origin: "guessed"}} {
		if _, e = NormalizeOperationsQuery(bad); !errors.Is(e, d.ErrInvestmentInvalid) {
			t.Fatal("bad query accepted", e)
		}
	}
}
func TestOperationSettlementMathUsesExactFacts(t *testing.T) {
	for _, tc := range []struct{ kind, amount, accrued, fee, tax, quantity, want string }{{"buy", "1", "0.1", "0.2", "0", "3", "-1.3"}, {"sell", "1", "0.1", "0.2", "0", "3", "0.9"}, {"dividend", "9007199254740993.123456789012", "0", "0", "0.000000000001", "0", "9007199254740993.123456789011"}, {"tax_refund", "0.000000000001", "0", "0", "0", "0", "0.000000000001"}} {
		o := op(tc.kind, tc.kind, tc.amount, 1)
		o.Accrued = tc.accrued
		o.Fee = tc.fee
		o.Tax = tc.tax
		o.Quantity = tc.quantity
		if tc.kind == "buy" || tc.kind == "sell" || tc.kind == "dividend" {
			o.InstrumentKey = "STOCK"
		}
		row, e := operationRow(o, "Broker", 0)
		if e != nil {
			t.Fatal(e)
		}
		if len(row.Cash) != 1 {
			t.Fatal(row)
		}
		expectPayoutRat(t, row.Cash[0].Amount, tc.want)
		if tc.kind == "buy" || tc.kind == "sell" {
			if row.UnitCleanPrice.Cmp(big.NewRat(1, 3)) != 0 {
				t.Fatal("unit price rounded")
			}
		}
	}
	o := op("fx", "fx", "100", 1)
	o.Fee = "1"
	o.ToCurrency = "EUR"
	o.ToAmount = "0.987654321012"
	row, e := operationRow(o, "Broker", 0)
	if e != nil || len(row.Cash) != 2 {
		t.Fatal(e, row)
	}
	expectPayoutRat(t, row.Cash[0].Amount, "-101")
	expectPayoutRat(t, row.Cash[1].Amount, "0.987654321012")
}
