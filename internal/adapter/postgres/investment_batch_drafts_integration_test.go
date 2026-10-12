package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

func TestInvestmentBatchDraftPersistence(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, otherTenant, actor, other, member := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{tenant, otherTenant} {
		if _, e := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Drafts','RUB')`, id); e != nil {
			t.Fatal(e)
		}
	}
	for _, id := range []string{actor, other, member} {
		if _, e := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, id, id+"@example.test"); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$3,'owner'),($1,$4,'admin'),($1,$5,'member'),($2,$3,'owner')`, tenant, otherTenant, actor, other, member); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Draft ledger", at, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := imp.PrepareUpload(imp.Upload{Filename: "draft.csv", RequestKey: "upload", CSV: []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nCASH_IN,2024-01-01 12:00:00,,100,1,RUB,0,,0,,False,\n"), Accounts: []imp.ImportAccount{{SourceKey: "a", Name: "A"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "a"}}})
	if e != nil {
		t.Fatal(e)
	}
	source, e := NewInvestmentImportRepo(pool).SaveImport(ctx, tenant, actor, prepared)
	if e != nil {
		t.Fatal(e)
	}
	batch := inv.ImportBatch{ImportID: source.ID, PreviewHash: prepared.Preview.Hash, RequestKey: "saved-draft", ExpectedImportVersion: 1, ExpectedLedgerVersions: map[string]int64{ledger.ID: 1}, Entries: []inv.ImportBatchEntry{{Record: 1, ExpectedRowVersion: 1, ReviewReason: "Reviewed cash", Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "9007199254740993.123456789012", OccurredAt: at}}}}
	write := inv.BatchDraftWrite{ID: uuid.NewString(), Batch: batch}
	draft, e := svc.SaveBatchDraft(ctx, tenant, actor, write)
	if e != nil || draft.Version != 1 {
		t.Fatal(draft, e)
	}
	retry, e := svc.SaveBatchDraft(ctx, tenant, actor, write)
	if e != nil || retry.Version != 1 || retry.ID != draft.ID {
		t.Fatal("save retry", retry, e)
	}
	if _, e = svc.GetBatchDraft(ctx, tenant, other, draft.ID); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("other author", e)
	}
	if _, e = svc.GetBatchDraft(ctx, otherTenant, actor, draft.ID); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("other tenant", e)
	}
	if _, e = repo.GetBatchDraft(ctx, tenant, member, draft.ID); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("member", e)
	}
	if _, e = svc.CommitBatchDraft(ctx, tenant, actor, draft.ID, 1, prepared.Preview.Hash); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("unprepared commit", e)
	}
	draft, e = svc.PrepareBatchDraft(ctx, tenant, actor, draft.ID, 1)
	if e != nil || draft.Preview == nil || draft.Preview.Committed {
		t.Fatal(draft, e)
	}
	oldHash := draft.Preview.Hash
	// Two tabs editing one version cannot silently overwrite each other.
	type outcome struct {
		draft inv.BatchDraft
		err   error
	}
	edits := make(chan outcome, 2)
	for _, note := range []string{"Tab A", "Tab B"} {
		go func(note string) {
			b := batch
			b.Entries = append([]inv.ImportBatchEntry(nil), batch.Entries...)
			b.Entries[0].Operation.Note = note
			updated, e := svc.SaveBatchDraft(ctx, tenant, actor, inv.BatchDraftWrite{ID: write.ID, ExpectedVersion: 1, Batch: b})
			edits <- outcome{updated, e}
		}(note)
	}
	a, b := <-edits, <-edits
	if a.err != nil {
		a, b = b, a
	}
	if a.err != nil || !errors.Is(b.err, d.ErrInvestmentConflict) || a.draft.Version != 2 || a.draft.Preview != nil {
		t.Fatal(a, b)
	}
	draft = a.draft
	if _, e = svc.CommitImportBatch(ctx, tenant, actor, batch, oldHash); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("legacy endpoint bypassed changed draft", e)
	}
	changedKey := draft.Batch
	changedKey.RequestKey = "changed-key"
	if _, e = svc.SaveBatchDraft(ctx, tenant, actor, inv.BatchDraftWrite{ID: draft.ID, ExpectedVersion: 2, Batch: changedKey}); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("draft request key changed", e)
	}

	if _, e = svc.CommitBatchDraft(ctx, tenant, actor, draft.ID, 1, oldHash); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("old confirmed draft committed", e)
	}
	draft, e = svc.PrepareBatchDraft(ctx, tenant, actor, draft.ID, 2)
	if e != nil {
		t.Fatal(e)
	}
	reloaded, e := NewInvestmentRepo(pool).GetBatchDraft(ctx, tenant, actor, draft.ID)
	if e != nil || reloaded.Preview == nil || reloaded.Preview.Hash != draft.Preview.Hash || reloaded.Batch.Entries[0].Operation.Amount != batch.Entries[0].Operation.Amount {
		t.Fatal("reload lost review", reloaded, e)
	}
	// Fail after financial writes, while updating the durable draft result.
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION fail_draft_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF (NEW.preview->>'Committed')::boolean THEN RAISE EXCEPTION 'synthetic result failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_draft_commit BEFORE UPDATE ON investment_batch_drafts FOR EACH ROW EXECUTE FUNCTION fail_draft_commit()`); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CommitBatchDraft(ctx, tenant, actor, draft.ID, 2, draft.Preview.Hash); e == nil {
		t.Fatal("partial commit")
	}
	var count int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("financial writes survived failed receipt", count, e)
	}
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM financial_write_requests WHERE tenant_id=$1 AND operation='investment.import.commit'`, tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("receipt survived rollback", count, e)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER fail_draft_commit ON investment_batch_drafts; DROP FUNCTION fail_draft_commit()`); e != nil {
		t.Fatal(e)
	}
	commits := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			saved, e := svc.CommitBatchDraft(ctx, tenant, actor, draft.ID, 2, draft.Preview.Hash)
			commits <- outcome{saved, e}
		}()
	}
	a, b = <-commits, <-commits
	if a.err != nil || b.err != nil || !a.draft.Preview.Committed || !b.draft.Preview.Committed || a.draft.Preview.Operations[0].ID != b.draft.Preview.Operations[0].ID {
		t.Fatal(a, b)
	}
	reloaded, e = NewInvestmentRepo(pool).GetBatchDraft(ctx, tenant, actor, draft.ID)
	if e != nil || !reloaded.Preview.Committed {
		t.Fatal("committed result unavailable after reconnect", e)
	}
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate posting", count, e)
	}
	list, e := svc.ListBatchDrafts(ctx, tenant, actor, "")
	if e != nil || len(list) != 1 || !list[0].Committed || list[0].Rows != 1 {
		t.Fatal(list, e)
	}
	list, e = svc.ListBatchDrafts(ctx, tenant, other, "")
	if e != nil || len(list) != 0 {
		t.Fatal("private drafts exposed", list, e)
	}
	if e = svc.DeleteBatchDraft(ctx, tenant, actor, draft.ID, 2); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("committed draft removed", e)
	}
	if _, e = svc.SaveBatchDraft(ctx, tenant, actor, inv.BatchDraftWrite{ID: draft.ID, ExpectedVersion: 2, Batch: draft.Batch}); e == nil {
		t.Fatal("committed draft changed")
	}

	cancel := inv.BatchCancellation{DraftID: draft.ID, ExpectedDraftVersion: 2, RequestKey: "cancel-draft", Reason: "Duplicate source review"}
	if _, e = svc.PreviewBatchCancellation(ctx, tenant, other, cancel); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("cancel other author", e)
	}
	if _, e = repo.CancelBatch(ctx, tenant, member, cancel, "", false); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("cancel member", e)
	}
	preview, e := svc.PreviewBatchCancellation(ctx, tenant, actor, cancel)
	if e != nil || preview.Cancelled || len(preview.Operations) != 1 || preview.Operations[0].Status != "voided" {
		t.Fatal("cancel preview", preview, e)
	}
	withdrawal, e := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "later-withdrawal", ExpectedLedgerVersion: 2, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "withdrawal", Currency: "RUB", Amount: "10", OccurredAt: at.AddDate(0, 0, 1)}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.PreviewBatchCancellation(ctx, tenant, actor, cancel); e == nil {
		t.Fatal("cancel broke dependent withdrawal")
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "void", RequestKey: "void-withdrawal", Reason: "Remove dependency", ExpectedLedgerVersion: 3, ExpectedOperationVersion: 1, Operation: withdrawal}); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CommitBatchCancellation(ctx, tenant, actor, cancel, preview.Hash); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale cancellation preview", e)
	}
	preview, e = svc.PreviewBatchCancellation(ctx, tenant, actor, cancel)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION fail_batch_cancel() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic cancellation receipt failure'; END $$; CREATE TRIGGER fail_batch_cancel BEFORE INSERT ON investment_batch_cancellations FOR EACH ROW EXECUTE FUNCTION fail_batch_cancel()`); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CommitBatchCancellation(ctx, tenant, actor, cancel, preview.Hash); e == nil {
		t.Fatal("receipt failure accepted")
	}
	var status string
	var opVersion int64
	if e = pool.DB.QueryRow(ctx, `SELECT status,version FROM investment_operations WHERE tenant_id=$1 AND id=$2`, tenant, reloaded.Preview.Operations[0].ID).Scan(&status, &opVersion); e != nil || status != "posted" || opVersion != 1 {
		t.Fatal("partial cancellation", status, opVersion, e)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER fail_batch_cancel ON investment_batch_cancellations; DROP FUNCTION fail_batch_cancel()`); e != nil {
		t.Fatal(e)
	}
	type cancelOutcome struct {
		result inv.BatchCancellationResult
		err    error
	}
	cancels := make(chan cancelOutcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			v, e := svc.CommitBatchCancellation(ctx, tenant, actor, cancel, preview.Hash)
			cancels <- cancelOutcome{v, e}
		}()
	}
	ca, cb := <-cancels, <-cancels
	if ca.err != nil || cb.err != nil || !ca.result.Cancelled || !cb.result.Cancelled || ca.result.Operations[0].Version != 2 || !ca.result.CancelledAt.Equal(cb.result.CancelledAt) {
		t.Fatal(ca, cb)
	}
	reloaded, e = svc.GetBatchDraft(ctx, tenant, actor, draft.ID)
	if e != nil || reloaded.Cancellation == nil || !reloaded.Cancellation.Cancelled {
		t.Fatal("lost durable cancellation", reloaded, e)
	}
	list, e = svc.ListBatchDrafts(ctx, tenant, actor, "")
	if e != nil || len(list) != 1 || !list[0].Cancelled {
		t.Fatal("cancel summary", list, e)
	}
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_source_links WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 1 {
		t.Fatal("source provenance removed", count, e)
	}
	cancel.Reason = "Different reason"
	if _, e = svc.CommitBatchCancellation(ctx, tenant, actor, cancel, preview.Hash); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("cancel retry changed reason", e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0039_investment_batch_cancellations")
	exported, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil || archiveRows(exported, "investment_batch_drafts") != 1 {
		t.Fatal("draft absent from archive", e)
	}
	if _, e = archive.VerifyJournal(exported.Data); e != nil {
		t.Fatal(e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0038_investment_batch_drafts")
}
