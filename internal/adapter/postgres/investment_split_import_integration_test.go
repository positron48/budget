package postgres

import (
	"context"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"strings"
	"testing"
	"time"
)

func TestInvestmentSplitSourcePreparationCommitAndRestore(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	journal := NewInvestmentRepo(pool)
	svc := inv.NewService(journal, NewTenantRepo(pool))
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Split", start, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "fund", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "1000", OccurredAt: start}}); e != nil {
		t.Fatal(e)
	}
	csv := []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nBUY,2024-01-02 12:00:00,S,100,1,RUB,0,X,0,,False,\nSPLIT,2024-01-03 03:00:00,S,10,0,RUB,0,X,0,,,\nSELL,2024-01-04 12:00:00,S,11,10,RUB,0,X,0,,False,\n")
	importer := imp.NewImportService(NewInvestmentImportRepo(pool), NewTenantRepo(pool))
	saved, e := importer.Upload(ctx, tenant, actor, imp.Upload{Filename: "split.csv", CSV: csv, RequestKey: "source", Accounts: []imp.ImportAccount{{SourceKey: "tinvest:account", Name: "Source"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "tinvest:account", Evidence: "Verified"}, {Record: 2, AccountKey: "tinvest:account", Evidence: "Verified"}, {Record: 3, AccountKey: "tinvest:account", Evidence: "Verified"}}})
	if e != nil {
		t.Fatal(e)
	}
	mapping := imp.FinancialMapping{SourceSHA256: saved.SourceSHA256, Timezone: "Europe/Moscow", SettlementPolicy: "assume_trade_time", LedgerByRecord: map[int]string{1: ledger.ID, 2: ledger.ID, 3: ledger.ID}, DateOnlyByRecord: map[int]bool{2: true}, SplitEvidence: map[int]imp.SplitEvidence{2: {BrokerAccountID: "wrong", InstrumentKey: "S@X", NewUnits: "10", OldUnits: "1", Reference: "Synthetic issuer terms", Reason: "Verified ratio and account"}}}
	for _, kind := range []string{"buy", "sell"} {
		mapping.Rules = append(mapping.Rules, imp.FinancialRule{Event: strings.ToUpper(kind), Symbol: "S", Exchange: "X", Currency: "RUB", Kind: kind, InstrumentKey: "S@X", AmountBasis: "quantity_price", AmountFactor: "1", QuantityFactor: "1", AccruedBasis: "zero_only", FeePolicy: "zero_only", Reason: "Verified synthetic trade"})
	}
	req := imp.FinancialReviewRequest{ImportID: saved.ID, ExpectedImportVersion: 1, RequestKey: "prepare", Mapping: mapping, Limit: 100}
	bad, e := importer.PreviewFinancial(ctx, tenant, actor, req)
	if e != nil || bad.Ready != 2 || bad.Unresolved != 1 {
		t.Fatal("wrong broker account accepted", bad, e)
	}
	evidence := mapping.SplitEvidence[2]
	evidence.BrokerAccountID = "account"
	mapping.SplitEvidence[2] = evidence
	req.Mapping = mapping
	prepared, e := importer.PreviewFinancial(ctx, tenant, actor, req)
	if e != nil || prepared.Ready != 3 || prepared.Batch == nil {
		t.Fatal(prepared, e)
	}
	preview, e := svc.PreviewImportBatch(ctx, tenant, actor, *prepared.Batch)
	if e != nil {
		t.Fatal(e)
	}
	posted, e := svc.CommitImportBatch(ctx, tenant, actor, *prepared.Batch, preview.Hash)
	if e != nil || len(posted.Operations) != 3 {
		t.Fatal(posted, e)
	}
	if retry, e := svc.CommitImportBatch(ctx, tenant, actor, *prepared.Batch, preview.Hash); e != nil || retry.Hash != posted.Hash {
		t.Fatal(retry, e)
	}
	afterSplit := time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)
	snapshot, e := svc.Snapshot(ctx, tenant, actor, ledger.ID, afterSplit)
	if e != nil || len(snapshot.Positions) != 1 || snapshot.Positions[0].Quantity != "10" || snapshot.Positions[0].CleanCost != "100" || snapshot.Cash[0].Economic != "900" {
		t.Fatal("split cash/cost changed", snapshot, e)
	}
	final, e := svc.Snapshot(ctx, tenant, actor, ledger.ID, afterSplit.AddDate(0, 0, 1))
	if e != nil || final.Cash[0].Economic != "1010" {
		t.Fatal(final, e)
	}
	bundle, e := journal.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal(e)
	}
	target, cleanup := withPg(t)
	defer cleanup()
	for _, q := range []struct {
		sql  string
		args []any
	}{{`INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restore','RUB')`, []any{tenant}}, {`INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, []any{actor, actor + "@example.test"}}, {`INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, []any{tenant, actor}}} {
		if _, e = target.DB.Exec(context.Background(), q.sql, q.args...); e != nil {
			t.Fatal(e)
		}
	}
	targetRepo := NewInvestmentRepo(target)
	if _, e = targetRepo.RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); e != nil {
		t.Fatal(e)
	}
	restored, e := inv.NewService(targetRepo, NewTenantRepo(target)).Snapshot(ctx, tenant, actor, ledger.ID, afterSplit)
	if e != nil || restored.Positions[0].Quantity != "10" || restored.Positions[0].CleanCost != "100" {
		t.Fatal(restored, e)
	}
}
