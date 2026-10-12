package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

func TestInvestmentImportBatchAtomicity(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, actor, member := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, e := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Batch','RUB')`, tenant); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{actor, member} {
		if _, e := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, id, id+"@example.test"); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, tenant, actor, member); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	a, e := svc.CreateLedger(ctx, tenant, actor, "A", at, "a")
	if e != nil {
		t.Fatal(e)
	}
	b, e := svc.CreateLedger(ctx, tenant, actor, "B", at, "b")
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := imp.PrepareUpload(imp.Upload{Filename: "batch.csv", RequestKey: "upload", CSV: []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nCASH_IN,2024-01-01 12:00:00,,100,1,RUB,0,,0,,False,\nBUY,2024-01-02 12:00:00,TEST,10,2,RUB,0,X,0,,False,\nCASH_IN,2024-01-01 12:00:00,,50,1,RUB,0,,0,,False,\n"), Accounts: []imp.ImportAccount{{SourceKey: "tinvest:a", Name: "A"}, {SourceKey: "tinvest:b", Name: "B"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "tinvest:a"}, {Record: 2, AccountKey: "tinvest:a"}, {Record: 3, AccountKey: "tinvest:b"}}})
	if e != nil {
		t.Fatal(e)
	}
	imported, e := NewInvestmentImportRepo(pool).SaveImport(ctx, tenant, actor, prepared)
	if e != nil {
		t.Fatal(e)
	}
	batch := inv.ImportBatch{ImportID: imported.ID, PreviewHash: prepared.Preview.Hash, ExpectedImportVersion: 1, RequestKey: "batch", ExpectedLedgerVersions: map[string]int64{a.ID: 1, b.ID: 1}, Entries: []inv.ImportBatchEntry{
		{Record: 1, ExpectedRowVersion: 1, ReviewReason: "Reviewed cash", Operation: d.InvestmentOperation{LedgerID: a.ID, Kind: "deposit", Currency: "RUB", Amount: "100", OccurredAt: at.Add(12 * time.Hour)}},
		{Record: 2, ExpectedRowVersion: 1, ReviewReason: "Reviewed trade", Operation: d.InvestmentOperation{LedgerID: a.ID, Kind: "buy", InstrumentKey: "TEST", Currency: "RUB", Amount: "20", Quantity: "2", OccurredAt: at.Add(36 * time.Hour)}},
		{Record: 3, ExpectedRowVersion: 1, ReviewReason: "Reviewed cash", Operation: d.InvestmentOperation{LedgerID: b.ID, Kind: "deposit", Currency: "RUB", Amount: "50", OccurredAt: at.Add(12 * time.Hour)}},
	}}
	count := func(table string, want int) {
		t.Helper()
		var n int
		if e := pool.DB.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", tenant).Scan(&n); e != nil || n != want {
			t.Fatal(table, n, e)
		}
	}
	if _, e = svc.PreviewImportBatch(ctx, tenant, member, batch); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("member preview", e)
	}
	if _, e = repo.ImportBatch(ctx, tenant, member, batch, "", false); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("repository member guard", e)
	}
	invalid := batch
	invalid.Entries = append([]inv.ImportBatchEntry(nil), batch.Entries...)
	invalid.Entries[1].Operation.Kind = "sell"
	if _, e = svc.PreviewImportBatch(ctx, tenant, actor, invalid); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("short accepted", e)
	}
	count("investment_operations", 0)
	count("investment_source_links", 0)
	preview, e := svc.PreviewImportBatch(ctx, tenant, actor, batch)
	if e != nil || preview.Committed || len(preview.Operations) != 3 || len(preview.Snapshots) != 2 {
		t.Fatal(preview, e)
	}
	count("investment_operations", 0)
	count("investment_import_ledger_bindings", 0)
	// Changing any affected journal invalidates the whole prepared batch.
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "intervening", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: a.ID, Kind: "deposit", Currency: "RUB", Amount: "1", OccurredAt: at}}); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CommitImportBatch(ctx, tenant, actor, batch, preview.Hash); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale journal accepted", e)
	}
	batch.ExpectedLedgerVersions[a.ID] = 2
	if _, e = svc.CommitImportBatch(ctx, tenant, actor, batch, preview.Hash); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("changed payload accepted", e)
	}
	preview, e = svc.PreviewImportBatch(ctx, tenant, actor, batch)
	if e != nil {
		t.Fatal(e)
	}
	mapping := imp.FinancialMapping{SourceSHA256: imported.SourceSHA256, Timezone: "UTC", SettlementPolicy: "assume_trade_time", LedgerByRecord: map[int]string{1: a.ID, 2: a.ID, 3: b.ID}, Rules: []imp.FinancialRule{
		{Event: "CASH_IN", Currency: "RUB", Kind: "deposit", AmountBasis: "price", AmountFactor: "1", FeePolicy: "zero_only", Reason: "Reviewed cash"},
		{Event: "BUY", Symbol: "TEST", Exchange: "X", Currency: "RUB", InstrumentKey: "TEST", Kind: "buy", AmountBasis: "quantity_price", QuantityFactor: "1", AmountFactor: "1", AccruedBasis: "zero_only", FeePolicy: "same_currency", Reason: "Reviewed trade"},
	}}
	importService := imp.NewImportService(NewInvestmentImportRepo(pool), NewTenantRepo(pool))
	reviewRequest := imp.FinancialReviewRequest{ImportID: imported.ID, ExpectedImportVersion: 1, RequestKey: "batch", Mapping: mapping, Limit: 5000}
	financial, e := importService.PreviewFinancial(ctx, tenant, actor, reviewRequest)
	if e != nil || financial.Ready != 3 || financial.Unresolved != 0 || financial.Batch == nil || financial.Batch.PreparationEvidenceJSON == "" {
		t.Fatal("financial preparation", financial, e)
	}
	count("investment_operations", 1)
	if _, e = importService.PreviewFinancial(ctx, tenant, member, reviewRequest); !errors.Is(e, imp.ErrForbidden) {
		t.Fatal("member financial preparation", e)
	}
	foreign := reviewRequest
	foreign.Mapping.LedgerByRecord = map[int]string{1: a.ID, 2: a.ID, 3: uuid.NewString()}
	blocked, e := importService.PreviewFinancial(ctx, tenant, actor, foreign)
	if e != nil || blocked.Ready != 2 || blocked.Unresolved != 1 || blocked.Rows[2].Operation != nil {
		t.Fatal("foreign journal accepted", blocked, e)
	}
	stale := reviewRequest
	stale.ExpectedImportVersion = 2
	if _, e = importService.PreviewFinancial(ctx, tenant, actor, stale); !errors.Is(e, imp.ErrConflict) {
		t.Fatal("stale financial review", e)
	}
	pageRequest := reviewRequest
	pageRequest.Limit = 1
	pageRequest.Offset = 1
	pageResult, e := importService.PreviewFinancial(ctx, tenant, actor, pageRequest)
	if e != nil || pageResult.Hash != financial.Hash || len(pageResult.Rows) != 1 || len(pageResult.Batch.Entries) != 1 || pageResult.Batch.Entries[0].Record != 2 {
		t.Fatal("financial pagination", pageResult, e)
	}
	brokerRequest := reviewRequest
	brokerRequest.Mapping.TradeEvidence = map[int]imp.TradeEvidence{2: {SourceRowID: imported.SourceSHA256 + ":2", BrokerAccountID: "a", BrokerOperationID: "trade", SnapshotSHA256: strings.Repeat("a", 64), ReportHash: strings.Repeat("b", 64), InstrumentUID: "11111111-1111-4111-8111-111111111111", InstrumentType: "share", Ticker: "TEST", Currency: "RUB", OperationType: "OPERATION_TYPE_BUY", State: "OPERATION_STATE_EXECUTED", Quantity: "2", Price: "10", Payment: "-20", Commission: "0", BrokerDate: at.Add(36 * time.Hour), Reason: "Matched broker cash"}}
	brokerReview, e := importService.PreviewFinancial(ctx, tenant, actor, brokerRequest)
	if e != nil || brokerReview.Ready != 3 || brokerReview.Rows[1].Operation.InstrumentKey != "tinvest:11111111-1111-4111-8111-111111111111" {
		t.Fatal("broker evidence", brokerReview, e)
	}
	wrongAccount := brokerRequest.Mapping.TradeEvidence[2]
	wrongAccount.BrokerAccountID = "b"
	brokerRequest.Mapping.TradeEvidence[2] = wrongAccount
	brokerReview, e = importService.PreviewFinancial(ctx, tenant, actor, brokerRequest)
	if e != nil || brokerReview.Ready != 2 || brokerReview.Rows[1].Operation != nil {
		t.Fatal("broker account evidence crossed accounts", brokerReview, e)
	}
	batch = *financial.Batch
	preview, e = svc.PreviewImportBatch(ctx, tenant, actor, batch)
	if e != nil {
		t.Fatal(e)
	}
	draft, e := svc.SaveBatchDraft(ctx, tenant, actor, inv.BatchDraftWrite{ID: uuid.NewString(), Batch: batch})
	if e != nil {
		t.Fatal(e)
	}
	draft, e = svc.PrepareBatchDraft(ctx, tenant, actor, draft.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION fail_batch_links() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic failure'; END $$; CREATE TRIGGER fail_batch_links BEFORE INSERT ON investment_source_links FOR EACH ROW EXECUTE FUNCTION fail_batch_links()`); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CommitImportBatch(ctx, tenant, actor, batch, preview.Hash); e == nil {
		t.Fatal("failed source writes committed")
	}
	count("investment_operations", 1)
	count("investment_operation_revisions", 1)
	count("investment_source_links", 0)
	count("investment_import_ledger_bindings", 0)
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER fail_batch_links ON investment_source_links; DROP FUNCTION fail_batch_links()`); e != nil {
		t.Fatal(e)
	}
	type outcome struct {
		result inv.ImportBatchResult
		err    error
	}
	completed := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			result, e := svc.CommitImportBatch(ctx, tenant, actor, batch, preview.Hash)
			completed <- outcome{result, e}
		}()
	}
	first, second := <-completed, <-completed
	if first.err != nil || second.err != nil || !first.result.Committed || !second.result.Committed || first.result.Operations[0].ID != second.result.Operations[0].ID {
		t.Fatal(first, second)
	}
	count("investment_operations", 4)
	count("investment_operation_revisions", 4)
	count("investment_source_links", 3)
	count("investment_import_ledger_bindings", 2)
	snapshot, e := svc.Snapshot(ctx, tenant, actor, a.ID, at.Add(72*time.Hour))
	if e != nil || snapshot.Ledger.Version != 4 || snapshot.Cash[0].Economic != "81" || snapshot.Positions[0].Quantity != "2" {
		t.Fatal(snapshot, e)
	}
	exported, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(exported.Data); e != nil {
		t.Fatal("batch audit invalid", e)
	}
	batch.RequestKey = "duplicate-source"
	batch.ExpectedLedgerVersions[a.ID] = 4
	batch.ExpectedLedgerVersions[b.ID] = 2
	if _, e = svc.PreviewImportBatch(ctx, tenant, actor, batch); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("consumed source accepted", e)
	}
	consumed, e := importService.PreviewFinancial(ctx, tenant, actor, reviewRequest)
	if e != nil || consumed.Ready != 0 || consumed.Unresolved != 3 || consumed.Batch != nil {
		t.Fatal("financial mapper reused posted source", consumed, e)
	}
	loaded, e := svc.GetBatchDraft(ctx, tenant, actor, draft.ID)
	if e != nil || loaded.Batch.PreparationEvidenceJSON != financial.Batch.PreparationEvidenceJSON {
		t.Fatal("preparation evidence lost", e)
	}
	// Cancellation uses amended heads, skips previously voided members and spans
	// all original ledgers. The unrelated initial deposit must survive.
	amended := first.result.Operations[1]
	amended.Amount = "30"
	amended, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "amend", RequestKey: "amend-member", Reason: "Correct total", ExpectedLedgerVersion: 4, ExpectedOperationVersion: 1, Operation: amended})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "void", RequestKey: "void-member", Reason: "Already cancelled", ExpectedLedgerVersion: 2, ExpectedOperationVersion: 1, Operation: first.result.Operations[2]}); e != nil {
		t.Fatal(e)
	}
	cancel := inv.BatchCancellation{DraftID: draft.ID, ExpectedDraftVersion: 1, RequestKey: "cancel-multiple", Reason: "Reverse reviewed batch"}
	cp, e := svc.PreviewBatchCancellation(ctx, tenant, actor, cancel)
	if e != nil || len(cp.Operations) != 2 || len(cp.AlreadyVoided) != 1 || len(cp.Snapshots) != 2 {
		t.Fatal(cp, e)
	}
	foundAmended := false
	for _, op := range cp.Operations {
		if op.ID == amended.ID {
			foundAmended = op.Amount == "30" && op.Version == 3
		}
	}
	if !foundAmended {
		t.Fatal("preview discarded amended head")
	}
	if _, e = svc.CommitBatchCancellation(ctx, tenant, actor, cancel, cp.Hash); e != nil {
		t.Fatal(e)
	}
	snapshot, e = svc.Snapshot(ctx, tenant, actor, a.ID, at.Add(72*time.Hour))
	if e != nil || snapshot.Ledger.Version != 7 || snapshot.Cash[0].Economic != "1" || len(snapshot.Positions) != 0 {
		t.Fatal(snapshot, e)
	}
	count("investment_operation_revisions", 8)
	count("investment_source_links", 3)
	exported, e = repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(exported.Data); e != nil {
		t.Fatal("cancelled batch audit", e)
	}

}

func TestInvestmentIncomeEvidenceAccountGuard(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	if _, err := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Income','RUB')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Income", at.Add(-time.Hour), "income")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := imp.PrepareUpload(imp.Upload{Filename: "income.csv", RequestKey: "income", CSV: []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nDIVIDEND,2024-01-01 12:00:00,TEST,0,100,RUB,13,X,0,,False,\n"), Accounts: []imp.ImportAccount{{SourceKey: "tinvest:a", Name: "A"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "tinvest:a"}}})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewInvestmentImportRepo(pool)
	imported, err := repo.SaveImport(ctx, tenant, actor, prepared)
	if err != nil {
		t.Fatal(err)
	}
	credit := imp.IncomeBrokerOperation{ID: "credit", AccountID: "a", Type: "OPERATION_TYPE_DIVIDEND", State: "OPERATION_STATE_EXECUTED", Ticker: "TEST", Currency: "RUB", Payment: "100", Date: at}
	tax := credit
	tax.ID = "tax"
	tax.Type = "OPERATION_TYPE_DIVIDEND_TAX"
	tax.Payment = "-13"
	evidence := imp.IncomeEvidence{SourceRowID: imported.SourceSHA256 + ":1", SnapshotSHA256: strings.Repeat("a", 64), ReportHash: strings.Repeat("b", 64), InstrumentUID: "11111111-1111-4111-8111-111111111111", Reason: "Reviewed cash and withholding", Credit: credit, Tax: tax}
	request := imp.FinancialReviewRequest{ImportID: imported.ID, ExpectedImportVersion: 1, RequestKey: "income", Limit: 100, Mapping: imp.FinancialMapping{SourceSHA256: imported.SourceSHA256, Timezone: "UTC", SettlementPolicy: "assume_trade_time", LedgerByRecord: map[int]string{1: ledger.ID}, IncomeEvidence: map[int]imp.IncomeEvidence{1: evidence}}}
	service := imp.NewImportService(repo, NewTenantRepo(pool))
	review, err := service.PreviewFinancial(ctx, tenant, actor, request)
	if err != nil || review.Ready != 1 || review.Batch == nil {
		t.Fatal(review, err)
	}
	replay, err := svc.PreviewImportBatch(ctx, tenant, actor, *review.Batch)
	if err != nil || len(replay.Snapshots) != 1 || replay.Snapshots[0].Cash[0].Economic != "87" {
		t.Fatal(replay, err)
	}
	evidence.Credit.AccountID = "b"
	evidence.Tax.AccountID = "b"
	request.Mapping.IncomeEvidence[1] = evidence
	rejected, err := service.PreviewFinancial(ctx, tenant, actor, request)
	if err != nil || rejected.Ready != 0 || rejected.Unresolved != 1 || rejected.Rows[0].Operation != nil || rejected.Hash == review.Hash {
		t.Fatal(rejected, err)
	}
	var count int
	if err := pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestInvestmentCashInterestPersistence(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	if _, err := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Income','RUB')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Income", at.Add(-time.Hour), "income")
	if err != nil {
		t.Fatal(err)
	}

	mutation := inv.Mutation{Action: "create", RequestKey: "interest", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "cash_interest", TaxReviewRequired: true, Amount: "2.123456789", Currency: "RUB", OccurredAt: at}}
	posted, err := svc.Mutate(ctx, tenant, actor, mutation)
	if err != nil {
		t.Fatal(err)
	}
	if !posted.TaxReviewRequired {
		t.Fatal("tax review metadata lost", posted)
	}
	again, err := svc.Mutate(ctx, tenant, actor, mutation)
	if err != nil || again.ID != posted.ID {
		t.Fatal(again, err)
	}
	snapshot, err := svc.Snapshot(ctx, tenant, actor, ledger.ID, at.Add(time.Hour))
	if err != nil || len(snapshot.Cash) != 1 || snapshot.Cash[0].Economic != "2.123456789" {
		t.Fatal(snapshot, err)
	}
	found := false
	for _, issue := range snapshot.Issues {
		if issue == "tax_review_required" {
			found = true
		}
	}
	if !found {
		t.Fatal("persisted tax review issue lost", snapshot)
	}
	var component, amount string
	if err := pool.DB.QueryRow(ctx, `SELECT component,amount::text FROM investment_cash_legs WHERE tenant_id=$1 AND operation_id=$2`, tenant, posted.ID).Scan(&component, &amount); err != nil || component != "cash_interest" || amount != "2.123456789000" {
		t.Fatal(component, amount, err)
	}
}

func TestInvestmentRepoIncomePersistence(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	if _, err := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Income','RUB')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Income", at.Add(-time.Hour), "income")
	if err != nil {
		t.Fatal(err)
	}

	mutation := inv.Mutation{Action: "create", RequestKey: "interest", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "repo_income", InstrumentKey: "tinvest:11111111-1111-4111-8111-111111111111", Amount: "2.123456789", Currency: "RUB", OccurredAt: at}}
	posted, err := svc.Mutate(ctx, tenant, actor, mutation)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Mutate(ctx, tenant, actor, mutation)
	if err != nil || again.ID != posted.ID {
		t.Fatal(again, err)
	}
	snapshot, err := svc.Snapshot(ctx, tenant, actor, ledger.ID, at.Add(time.Hour))
	if err != nil || len(snapshot.Cash) != 1 || snapshot.Cash[0].Economic != "2.123456789" {
		t.Fatal(snapshot, err)
	}
	var component, amount string
	if err := pool.DB.QueryRow(ctx, `SELECT component,amount::text FROM investment_cash_legs WHERE tenant_id=$1 AND operation_id=$2`, tenant, posted.ID).Scan(&component, &amount); err != nil || component != "income" || amount != "2.123456789000" {
		t.Fatal(component, amount, err)
	}
}

func TestInvestmentCashEvidenceAccountGuard(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	if _, err := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Income','RUB')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Income", at.Add(-time.Hour), "income")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := imp.PrepareUpload(imp.Upload{Filename: "income.csv", RequestKey: "income", CSV: []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nCASH_GAIN,2024-01-01 12:00:00,RUB,1,100,RUB,0,X,0,,False,\n"), Accounts: []imp.ImportAccount{{SourceKey: "tinvest:a", Name: "A"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "tinvest:a"}}})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewInvestmentImportRepo(pool)
	imported, err := repo.SaveImport(ctx, tenant, actor, prepared)
	if err != nil {
		t.Fatal(err)
	}
	evidence := imp.CashEvidence{SourceRowID: imported.SourceSHA256 + ":1", SnapshotSHA256: strings.Repeat("a", 64), ReportHash: strings.Repeat("b", 64), Reason: "Reviewed repo cash", InstrumentUID: "11111111-1111-4111-8111-111111111111", InstrumentType: "share", Operation: imp.IncomeBrokerOperation{ID: "repo", AccountID: "a", Type: "OPERATION_TYPE_OVERNIGHT", State: "OPERATION_STATE_EXECUTED", Ticker: "TEST", Currency: "RUB", Payment: "100", Date: at}}
	request := imp.FinancialReviewRequest{ImportID: imported.ID, ExpectedImportVersion: 1, RequestKey: "income", Limit: 100, Mapping: imp.FinancialMapping{SourceSHA256: imported.SourceSHA256, Timezone: "UTC", SettlementPolicy: "assume_trade_time", LedgerByRecord: map[int]string{1: ledger.ID}, CashEvidence: map[int]imp.CashEvidence{1: evidence}}}
	service := imp.NewImportService(repo, NewTenantRepo(pool))
	review, err := service.PreviewFinancial(ctx, tenant, actor, request)
	if err != nil || review.Ready != 1 || review.Batch == nil {
		t.Fatal(review, err)
	}
	replay, err := svc.PreviewImportBatch(ctx, tenant, actor, *review.Batch)
	if err != nil || len(replay.Snapshots) != 1 || replay.Snapshots[0].Cash[0].Economic != "100" {
		t.Fatal(replay, err)
	}
	evidence.Operation.AccountID = "b"
	request.Mapping.CashEvidence[1] = evidence
	rejected, err := service.PreviewFinancial(ctx, tenant, actor, request)
	if err != nil || rejected.Ready != 0 || rejected.Unresolved != 1 || rejected.Rows[0].Operation != nil || rejected.Hash == review.Hash {
		t.Fatal(rejected, err)
	}
	var count int
	if err := pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
