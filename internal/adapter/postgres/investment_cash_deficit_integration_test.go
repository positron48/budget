package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"slices"
	"testing"
	"time"
)

func TestInvestmentImportedCashDeficitPreviewBatchAuditAndRestore(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx := context.Background()
	tenant, user := uuid.NewString(), uuid.NewString()
	seed := func(p *Pool) {
		t.Helper()
		if _, e := p.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Imported cash','USD')`, tenant); e != nil {
			t.Fatal(e)
		}
		if _, e := p.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'cash-deficit@example.test','x')`, user); e != nil {
			t.Fatal(e)
		}
		if _, e := p.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
			t.Fatal(e)
		}
	}
	seed(pool)
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, user, "Broker", start, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := imp.PrepareUpload(imp.Upload{Filename: "cash-deficit.csv", RequestKey: "upload", CSV: []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nBUY,2024-01-02 12:00:00,stock,100,1,USD,0,X,0,,False,\nBUY,2024-01-03 12:00:00,stock,20,1,USD,0,X,0,,False,\n"), Accounts: []imp.ImportAccount{{SourceKey: "other", Name: "Other"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "other"}, {Record: 2, AccountKey: "other"}}})
	if e != nil {
		t.Fatal(e)
	}
	imported, e := NewInvestmentImportRepo(pool).SaveImport(ctx, tenant, user, prepared)
	if e != nil {
		t.Fatal(e)
	}
	op := d.InvestmentOperation{LedgerID: ledger.ID, Kind: "buy", Currency: "USD", InstrumentKey: "STOCK", Quantity: "1", Amount: "100", OccurredAt: start.Add(36 * time.Hour), SettlementAt: start.Add(72 * time.Hour)}
	m := inv.Mutation{Action: "create", ExpectedLedgerVersion: 1, RequestKey: "first", Operation: op, Source: &d.InvestmentSource{ImportID: imported.ID, Record: 1, ExpectedVersion: 1, ReviewReason: "Confirmed source row"}}
	blocked, e := svc.PreviewMutation(ctx, tenant, user, m)
	if e != nil || blocked.Valid {
		t.Fatal(blocked, e)
	}
	if _, e = svc.Mutate(ctx, tenant, user, m); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(e)
	}
	m.Operation.ImportedCashDeficitReason = "Missing funding in export; preserve actual trade"
	checked, e := svc.PreviewMutation(ctx, tenant, user, m)
	if e != nil || !checked.Valid || checked.Hash == "" {
		t.Fatal(checked, e)
	}
	m.PreviewHash = checked.Hash
	posted, e := svc.Mutate(ctx, tenant, user, m)
	if e != nil || posted.Source == nil {
		t.Fatal(posted, e)
	}
	retry, e := svc.Mutate(ctx, tenant, user, m)
	if e != nil || retry.ID != posted.ID {
		t.Fatal(retry, e)
	}
	// A forged source supplied inside the operation cannot bless a manual debit.
	ordinary := inv.Mutation{Action: "create", ExpectedLedgerVersion: 2, RequestKey: "manual", Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "withdrawal", Currency: "USD", Amount: "1", OccurredAt: start.Add(96 * time.Hour), Source: posted.Source}}
	if _, e = svc.Mutate(ctx, tenant, user, ordinary); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(e)
	}
	ordinary.Operation.ImportedCashDeficitReason = "Try forged source"
	if _, e = svc.Mutate(ctx, tenant, user, ordinary); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal(e)
	}
	// A foreign import ID is checked before the exception reaches replay.
	foreign := m
	foreign.RequestKey = "foreign"
	foreign.PreviewHash = ""
	foreign.ExpectedLedgerVersion = 2
	source := *m.Source
	source.ImportID = uuid.NewString()
	foreign.Source = &source
	if _, e = svc.PreviewMutation(ctx, tenant, user, foreign); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal(e)
	}
	// Batch uses the same replay policy after independently binding each row.
	second := op
	second.Amount = "20"
	second.OccurredAt = start.Add(60 * time.Hour)
	second.SettlementAt = start.Add(96 * time.Hour)
	batch := inv.ImportBatch{ImportID: imported.ID, PreviewHash: prepared.Preview.Hash, ExpectedImportVersion: 1, RequestKey: "second", ExpectedLedgerVersions: map[string]int64{ledger.ID: 2}, Entries: []inv.ImportBatchEntry{{Record: 2, ExpectedRowVersion: 1, ReviewReason: "Confirmed second row", Operation: second}}}
	if _, e = svc.PreviewImportBatch(ctx, tenant, user, batch); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(e)
	}
	batch.Entries[0].Operation.ImportedCashDeficitReason = "Same missing funding in original export"
	preview, e := svc.PreviewImportBatch(ctx, tenant, user, batch)
	if e != nil {
		t.Fatal(e)
	}
	committed, e := svc.CommitImportBatch(ctx, tenant, user, batch, preview.Hash)
	if e != nil || !committed.Committed {
		t.Fatal(committed, e)
	}
	snap, e := svc.Snapshot(ctx, tenant, user, ledger.ID, start.Add(120*time.Hour))
	if e != nil || snap.Cash[0].Settled != "-120" || !slices.Contains(snap.Issues, "negative_imported_cash") {
		t.Fatal(snap, e)
	}
	// Removing the exception is rejected until the historical funding is fixed.
	amend := inv.Mutation{Action: "amend", ExpectedLedgerVersion: 3, ExpectedOperationVersion: 1, RequestKey: "remove", Reason: "Review missing funding", Operation: posted}
	amend.Operation.ImportedCashDeficitReason = ""
	invalid, e := svc.PreviewMutation(ctx, tenant, user, amend)
	if e != nil || invalid.Valid {
		t.Fatal(invalid, e)
	}
	if _, e = svc.Mutate(ctx, tenant, user, amend); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(e)
	}
	// Reason corrections preserve source provenance and immutable audit history.
	amend.Operation.ImportedCashDeficitReason = "Corrected explanation for incomplete export"
	fixed, e := svc.Mutate(ctx, tenant, user, amend)
	if e != nil || fixed.Source.ImportID != imported.ID {
		t.Fatal(fixed, e)
	}
	var original string
	if e = pool.DB.QueryRow(ctx, `SELECT after_data->>'ImportedCashDeficitReason' FROM investment_operation_revisions WHERE tenant_id=$1 AND operation_id=$2 AND version=1`, tenant, posted.ID).Scan(&original); e != nil || original != m.Operation.ImportedCashDeficitReason {
		t.Fatal(original, e)
	}
	bundle, e := repo.ExportInvestmentArchive(ctx, tenant, user)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal(e)
	}

	// Removing the reviewed exception from a copy invalidates deterministic
	// replay even if the signed materialized legs are left unchanged.
	altered := bundle.Data
	altered.Tables = append([]archive.Table(nil), bundle.Data.Tables...)
	for i, table := range altered.Tables {
		if table.Name != "investment_operations" {
			continue
		}
		altered.Tables[i].Rows = append([]json.RawMessage(nil), table.Rows...)
		for j, raw := range table.Rows {
			var row map[string]json.RawMessage
			if e = json.Unmarshal(raw, &row); e != nil {
				t.Fatal(e)
			}
			var operation d.InvestmentOperation
			if e = json.Unmarshal(row["data"], &operation); e != nil {
				t.Fatal(e)
			}
			if operation.ID != posted.ID {
				continue
			}
			operation.ImportedCashDeficitReason = ""
			row["data"], _ = json.Marshal(operation)
			altered.Tables[i].Rows[j], _ = json.Marshal(row)
		}
	}
	if _, e = archive.VerifyJournal(altered); e == nil {
		t.Fatal("removed deficit exception accepted")
	}
	altered = bundle.Data
	altered.Tables = append([]archive.Table(nil), bundle.Data.Tables...)
	for i, table := range altered.Tables {
		if table.Name == "investment_source_links" {
			altered.Tables[i].Rows = nil
		}
	}
	if _, e = archive.VerifyJournal(altered); e == nil {
		t.Fatal("missing consumed source links accepted")
	}
	altered = bundle.Data
	altered.Tables = append([]archive.Table(nil), bundle.Data.Tables...)
	for i, table := range altered.Tables {
		if table.Name == "investment_import_rows" {
			altered.Tables[i].Rows = nil
		}
	}
	if _, e = archive.VerifyJournal(altered); e == nil {
		t.Fatal("missing original source rows accepted")
	}
	target, cleanup := withPg(t)
	defer cleanup()
	seed(target)
	restored := NewInvestmentRepo(target)
	if _, e = restored.CheckInvestmentArchiveRestore(ctx, user, bundle); e != nil {
		t.Fatal(e)
	}
	if _, e = restored.RestoreInvestmentArchive(ctx, user, bundle, bundle.SHA256); e != nil {
		t.Fatal(e)
	}
	got, e := restored.Snapshot(ctx, tenant, ledger.ID, start.Add(120*time.Hour))
	if e != nil || got.Cash[0].Settled != "-120" || !slices.Contains(got.Issues, "imported_cash_history_incomplete") {
		t.Fatal(got, e)
	}
}
