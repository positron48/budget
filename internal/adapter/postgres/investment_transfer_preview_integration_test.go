package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	"reflect"
	"testing"
	"time"
)

func TestInvestmentTransferPreviewCivilDatesVersionAndArchive(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx := context.Background()
	tenant, user := uuid.NewString(), uuid.NewString()
	if _, e := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Preview transfers','USD')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'transfer-preview@example.test','x')`, user); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	a, e := svc.CreateLedger(ctx, tenant, user, "Source", start, "a")
	if e != nil {
		t.Fatal(e)
	}
	b, e := svc.CreateLedger(ctx, tenant, user, "Target", start, "b")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "fund", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: a.ID, Kind: "deposit", Currency: "USD", Amount: "100", OccurredAt: start}}); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "buy", ExpectedLedgerVersion: 2, Operation: d.InvestmentOperation{LedgerID: a.ID, Kind: "buy", InstrumentKey: "S", Currency: "USD", Quantity: "2", Amount: "20", OccurredAt: start.Add(time.Hour)}}); e != nil {
		t.Fatal(e)
	}
	m := inv.TransferMutation{Action: "create", RequestKey: "date-cash", ExpectedFromVersion: 3, ExpectedToVersion: 1, Transfer: d.InvestmentTransfer{FromLedgerID: a.ID, ToLedgerID: b.ID, Currency: "USD", Amount: "10", Timing: &d.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-02", SourceDate: "2024-01-02"}}}
	preview, e := svc.PreviewTransfer(ctx, tenant, user, m)
	if e != nil || !preview.Preview.Valid || preview.Transfer.OccurredAt.Format(time.RFC3339Nano) != "2024-01-02T20:59:59.999999Z" {
		t.Fatal(preview, e)
	}
	var count int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_transfers WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
	// A metadata revision alone invalidates a reviewed paired mutation.
	if _, e = svc.UpdateLedger(ctx, tenant, user, inv.LedgerUpdate{ID: b.ID, Action: "edit", Name: "Renamed", AccountType: b.AccountType, DefaultCurrency: b.DefaultCurrency, ExpectedMetadataVersion: b.MetadataVersion, RequestKey: "rename", Reason: "Name reviewed"}); e != nil {
		t.Fatal(e)
	}
	m.PreviewHash = preview.Preview.Hash
	if _, e = svc.Transfer(ctx, tenant, user, m); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale metadata", e)
	}
	preview, e = svc.PreviewTransfer(ctx, tenant, user, m)
	if e != nil || !preview.Preview.Valid {
		t.Fatal(preview, e)
	}
	m.PreviewHash = preview.Preview.Hash
	cash, e := svc.Transfer(ctx, tenant, user, m)
	if e != nil || cash.ID != preview.Transfer.ID || cash.FromOperationID != preview.Transfer.FromOperationID || !reflect.DeepEqual(cash.Timing, m.Transfer.Timing) {
		t.Fatal(cash, e)
	}
	if retry, e := svc.Transfer(ctx, tenant, user, m); e != nil || retry.ID != cash.ID {
		t.Fatal(retry, e)
	}
	// Securities use civil start and preserve FIFO lineage/cost through the pair.
	security := inv.TransferMutation{Action: "create", RequestKey: "date-security", ExpectedFromVersion: 4, ExpectedToVersion: 2, Transfer: d.InvestmentTransfer{FromLedgerID: a.ID, ToLedgerID: b.ID, Currency: "USD", InstrumentKey: "S", Quantity: "1", Timing: &d.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-03"}}}
	p, e := svc.PreviewTransfer(ctx, tenant, user, security)
	if e != nil || !p.Preview.Valid || p.Transfer.OccurredAt.Format(time.RFC3339) != "2024-01-02T21:00:00Z" {
		t.Fatal(p, e)
	}
	security.PreviewHash = p.Preview.Hash
	securities, e := svc.Transfer(ctx, tenant, user, security)
	if e != nil {
		t.Fatal(e)
	}
	ops, e := svc.Operations(ctx, tenant, user, b.ID, 100, 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, o := range ops {
		if o.TransferID == securities.ID && !reflect.DeepEqual(o.Timing, securities.Timing) {
			t.Fatal(o)
		}
	}
	// Amend moves both cash legs; an invalid amount has a structured preview.
	amend := m
	amend.Action = "amend"
	amend.Transfer = cash
	amend.Transfer.Amount = "1000"
	amend.ExpectedVersion = 1
	amend.ExpectedFromVersion = 5
	amend.ExpectedToVersion = 3
	amend.RequestKey = "amend"
	amend.Reason = "Correct date"
	amend.PreviewHash = ""
	bad, e := svc.PreviewTransfer(ctx, tenant, user, amend)
	if e != nil || bad.Preview.Valid || len(bad.Preview.Issues) != 1 {
		t.Fatal(bad, e)
	}
	amend.Transfer.Amount = "10"
	amend.Transfer.OccurredAt = time.Time{}
	timing := *cash.Timing
	timing.OccurredDate = "2024-01-04"
	timing.SourceDate = ""
	amend.Transfer.Timing = &timing
	p, e = svc.PreviewTransfer(ctx, tenant, user, amend)
	if e != nil || !p.Preview.Valid {
		t.Fatal(p, e)
	}
	amend.PreviewHash = p.Preview.Hash
	cash, e = svc.Transfer(ctx, tenant, user, amend)
	if e != nil {
		t.Fatal(e)
	}
	cancel := amend
	cancel.Action = "void"
	cancel.Transfer = cash
	cancel.ExpectedVersion = 2
	cancel.ExpectedFromVersion = 6
	cancel.ExpectedToVersion = 4
	cancel.RequestKey = "void"
	cancel.PreviewHash = ""
	p, e = svc.PreviewTransfer(ctx, tenant, user, cancel)
	if e != nil || !p.Preview.Valid {
		t.Fatal(p, e)
	}
	cancel.PreviewHash = p.Preview.Hash
	if _, e = svc.Transfer(ctx, tenant, user, cancel); e != nil {
		t.Fatal(e)
	}
	bundle, e := repo.ExportInvestmentArchive(ctx, tenant, user)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal(e)
	}
	target, cleanup := withPg(t)
	defer cleanup()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restored transfers','USD')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'restored-transfers@example.test','x')`, user); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	restored := NewInvestmentRepo(target)
	if _, e = restored.RestoreInvestmentArchive(ctx, user, bundle, bundle.SHA256); e != nil {
		t.Fatal(e)
	}
	restoredTransfer, e := restored.GetTransfer(ctx, tenant, securities.ID)
	if e != nil || !reflect.DeepEqual(restoredTransfer.Timing, securities.Timing) {
		t.Fatal(restoredTransfer, e)
	}
	restoredSnapshot, e := restored.Snapshot(ctx, tenant, b.ID, start.AddDate(0, 0, 10))
	if e != nil || len(restoredSnapshot.Positions) != 1 || restoredSnapshot.Positions[0].Quantity != "1" || restoredSnapshot.Positions[0].CleanCost != "10" {
		t.Fatal(restoredSnapshot, e)
	}

	altered := bundle.Data
	raw, _ := json.Marshal(bundle.Data)
	if e = json.Unmarshal(raw, &altered); e != nil {
		t.Fatal(e)
	}
	for i, table := range altered.Tables {
		if table.Name != "investment_transfers" {
			continue
		}
		for j, row := range table.Rows {
			var v map[string]json.RawMessage
			if err := json.Unmarshal(row, &v); err != nil {
				t.Fatal(err)
			}
			var tr d.InvestmentTransfer
			if err := json.Unmarshal(v["data"], &tr); err != nil {
				t.Fatal(err)
			}
			if tr.ID != securities.ID {
				continue
			}
			tr.Timing = nil
			v["data"], _ = json.Marshal(tr)
			altered.Tables[i].Rows[j], _ = json.Marshal(v)
		}
	}
	if _, e = archive.VerifyJournal(altered); e == nil {
		t.Fatal("lost pair timing accepted")
	}
}
