package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentArchiveRebuild(t *testing.T) {
	source, cleanup := withPg(t)
	defer cleanup()
	target, cleanupTarget := withPg(t)
	defer cleanupTarget()
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	for _, pool := range []*Pool{source, target} {
		if _, e := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Archive rebuild','RUB')`, tenant); e != nil {
			t.Fatal(e)
		}
		if _, e := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'archive-rebuild@example.test','test')`, actor); e != nil {
			t.Fatal(e)
		}
		if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
			t.Fatal(e)
		}
	}
	svc := inv.NewService(NewInvestmentRepo(source), NewTenantRepo(source))
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Restored", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "ledger")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "100", OccurredAt: time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)}}); e != nil {
		t.Fatal(e)
	}
	market := NewMarketDataRepo(source)
	if _, e = market.RegisterPriceConventions(ctx, tenant, actor, nav.ConventionProfile{Key: "reviewed", Title: "Reviewed", Evidence: "Synthetic", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{}}, 0); e != nil {
		t.Fatal(e)
	}
	if _, e = market.RegisterFXPolicy(ctx, tenant, actor, md.FXPolicy{Key: "market", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}, 0); e != nil {
		t.Fatal(e)
	}
	portfolio, e := market.SaveInvestmentPortfolio(ctx, tenant, actor, nav.PortfolioInput{Name: "Restored", LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market", ConventionKey: "reviewed", From: "2024-01-01", To: "2024-01-03", RequestKey: "portfolio"})
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := imp.PrepareUpload(imp.Upload{Filename: "synthetic.csv", RequestKey: "upload", CSV: []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nBUY,2020-01-01 12:00:00,TEST,1,2,USD,0,X,0,,False,\n"), Accounts: []imp.ImportAccount{{SourceKey: "a", Name: "A"}, {SourceKey: "b", Name: "B"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "a"}}})
	if e != nil {
		t.Fatal(e)
	}
	imports := NewInvestmentImportRepo(source)
	imported, e := imports.SaveImport(ctx, tenant, actor, prepared)
	if e != nil {
		t.Fatal(e)
	}
	records, e := imports.ListImportRows(ctx, tenant, imported.ID, 10, 0)
	if e != nil {
		t.Fatal(e)
	}
	original := records[0].AccountID
	var other string
	for _, account := range imported.Accounts {
		if account.ID != original {
			other = account.ID
		}
	}
	if _, e = imports.ReassignImportRow(ctx, tenant, actor, imported.ID, 1, other, 1, "Synthetic"); e != nil {
		t.Fatal(e)
	}
	// A restored sequence must advance past explicit historical IDs.
	if _, e = source.DB.Exec(ctx, `UPDATE investment_import_mapping_revisions SET id=900000 WHERE tenant_id=$1`, tenant); e != nil {
		t.Fatal(e)
	}
	savedDraft, e := svc.SaveBatchDraft(ctx, tenant, actor, inv.BatchDraftWrite{ID: uuid.NewString(), Batch: inv.ImportBatch{ImportID: imported.ID, PreviewHash: prepared.Preview.Hash, RequestKey: "restore-review", ExpectedImportVersion: 2, ExpectedLedgerVersions: map[string]int64{ledger.ID: 2}, Entries: []inv.ImportBatchEntry{{Record: 1, ExpectedRowVersion: 2, ReviewReason: "Synthetic review", Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "TEST", Currency: "RUB", Amount: "20", Quantity: "2", OccurredAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)}}}}})
	if e != nil {
		t.Fatal(e)
	}
	savedDraft, e = svc.PrepareBatchDraft(ctx, tenant, actor, savedDraft.ID, savedDraft.Version)
	if e != nil {
		t.Fatal(e)
	}
	bundle, e := NewInvestmentRepo(source).ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(target)
	assertEmpty := func() {
		t.Helper()
		snapshot, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
		if e != nil {
			t.Fatal(e)
		}
		for _, table := range snapshot.Data.Tables {
			if len(table.Rows) != 0 {
				t.Fatal("partial restore", table.Name)
			}
		}
		var last int64
		var called bool
		if e = target.DB.QueryRow(ctx, `SELECT last_value,is_called FROM investment_import_mapping_revisions_id_seq`).Scan(&last, &called); e != nil || last != 1 || called {
			t.Fatal("sequence escaped rollback", last, called, e)
		}
		var count int
		if e = target.DB.QueryRow(ctx, `SELECT count(*) FROM investment_nav_jobs WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 0 {
			t.Fatal("jobs escaped rollback", count, e)
		}
	}
	if _, e = repo.CheckInvestmentArchiveRestore(ctx, actor, bundle); e != nil {
		t.Fatal("dry run", e)
	}
	assertEmpty()
	if _, e = target.DB.Exec(ctx, `CREATE FUNCTION fail_restore_jobs() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic failure'; END $$; CREATE TRIGGER fail_restore_jobs BEFORE INSERT ON investment_nav_jobs FOR EACH ROW EXECUTE FUNCTION fail_restore_jobs()`); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); e == nil {
		t.Fatal("queue failure ignored")
	}
	assertEmpty()
	if _, e = target.DB.Exec(ctx, `DROP TRIGGER fail_restore_jobs ON investment_nav_jobs; DROP FUNCTION fail_restore_jobs()`); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); e != nil {
		t.Fatal("apply", e)
	}
	restoredDraft, e := repo.GetBatchDraft(ctx, tenant, actor, savedDraft.ID)
	if e != nil || restoredDraft.Preview == nil || restoredDraft.Preview.Hash != savedDraft.Preview.Hash || restoredDraft.Preview.Committed || restoredDraft.Batch.Entries[0].Operation.Amount != "20" {
		t.Fatal("saved review not restored", restoredDraft, e)
	}
	if _, e = NewInvestmentImportRepo(target).ReassignImportRow(ctx, tenant, actor, imported.ID, 1, original, 2, "After restore"); e != nil {
		t.Fatal("serial collision", e)
	}
	var newID int64
	if e = target.DB.QueryRow(ctx, `SELECT id FROM investment_import_mapping_revisions WHERE tenant_id=$1 AND version=3`, tenant).Scan(&newID); e != nil || newID != 900001 {
		t.Fatal(newID, e)
	}
	restoredMarket := NewMarketDataRepo(target)
	for i := 0; i < 3; i++ {
		if worked, e := restoredMarket.RunDailyNAVOnce(ctx, tenant); e != nil || !worked {
			t.Fatal(worked, e)
		}
	}
	history, e := restoredMarket.NAVHistoryForSeries(ctx, tenant, actor, portfolio.SeriesKey, "2024-01-01", "2024-01-03")
	if e != nil || len(history) != 3 {
		t.Fatal(history, e)
	}
	for _, point := range history {
		if point.State != "ready" || !point.Result.Complete || point.Result.KnownValue.RatString() != "100" {
			t.Fatal("restored NAV", point)
		}
	}
}
