package postgres

import (
	"slices"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

func TestInvestmentPendingTradesDoNotDiscardKnownPayoutsOrClaimCompleteHistory(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	importer := imp.NewImportService(NewInvestmentImportRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Incomplete source", StartedAt: at, PortfolioTracking: true}, "partial")
	if e != nil {
		t.Fatal(e)
	}
	source, e := importer.Upload(ctx, tenant, actor, imp.Upload{Filename: "partial.csv", RequestKey: "partial", CSV: []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nBUY,2024-01-02 12:00:00,TEST,10,1,RUB,0,MCX,0,,False,\nSELL,2024-01-03 12:00:00,TEST,20,2,RUB,0,MCX,0,,False,\nDIVIDEND,2024-01-04 12:00:00,TEST,0,7,RUB,0,MCX,0,,False,\nCASH_IN,2024-01-04 12:00:00,,100,1,RUB,0,,0,,False,\n"), Accounts: []imp.ImportAccount{{SourceKey: "one", Name: "One"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "one"}, {Record: 2, AccountKey: "one"}, {Record: 3, AccountKey: "one"}, {Record: 4, AccountKey: "one"}}})
	if e != nil {
		t.Fatal(e)
	}
	mapping := imp.FinancialMapping{SourceSHA256: source.SourceSHA256, Timezone: "Europe/Moscow", SettlementPolicy: "assume_trade_time", LedgerByRecord: map[int]string{1: ledger.ID, 2: ledger.ID, 3: ledger.ID}, Rules: []imp.FinancialRule{{Event: "BUY", Symbol: "TEST", Exchange: "MCX", Currency: "RUB", InstrumentKey: "TEST", Kind: "buy", AmountBasis: "quantity_price", QuantityFactor: "1", AmountFactor: "1", AccruedBasis: "per_security_unit", FeePolicy: "native_currency", Reason: "Synthetic source"}, {Event: "SELL", Symbol: "TEST", Exchange: "MCX", Currency: "RUB", InstrumentKey: "TEST", Kind: "sell", AmountBasis: "quantity_price", QuantityFactor: "1", AmountFactor: "1", AccruedBasis: "per_security_unit", FeePolicy: "native_currency", Reason: "Synthetic source"}, {Event: "DIVIDEND", Symbol: "TEST", Exchange: "MCX", Currency: "RUB", InstrumentKey: "TEST", Kind: "dividend", AmountBasis: "quantity", AmountFactor: "1", FeePolicy: "native_currency", IncomeBasis: "received", Reason: "Known payout"}}}
	mapped, e := importer.PreviewFinancial(ctx, tenant, actor, imp.FinancialReviewRequest{ImportID: source.ID, ExpectedImportVersion: source.Version, RequestKey: "review", Mapping: mapping, Limit: 5000})
	if e != nil || mapped.Batch == nil {
		t.Fatal(e)
	}
	batch := *mapped.Batch
	batch.Entries = slices.DeleteFunc(slices.Clone(batch.Entries), func(e inv.ImportBatchEntry) bool { return e.Record != 3 })
	preview, e := svc.PreviewImportBatch(ctx, tenant, actor, batch)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CommitImportBatch(ctx, tenant, actor, batch, preview.Hash); e != nil {
		t.Fatal(e)
	}
	state, e := svc.Snapshot(ctx, tenant, actor, ledger.ID, at.AddDate(0, 0, 5))
	if e != nil || !slices.Contains(state.Issues, "source_trades_pending") || len(state.Positions) != 1 || state.Positions[0].Quantity != "0" {
		t.Fatal("partial history was hidden or payout created securities", e)
	}
	ops, e := svc.Operations(ctx, tenant, actor, ledger.ID, 200, 0)
	if e != nil || len(ops) != 1 || ops[0].Amount != "7" || ops[0].Kind != "dividend" {
		t.Fatal("known payout lost", e)
	}
	earlier, e := svc.Snapshot(ctx, tenant, actor, ledger.ID, at)
	if e != nil || slices.Contains(earlier.Issues, "source_trades_pending") {
		t.Fatal("future source trades polluted earlier date", e)
	}
	plan, e := pool.DB.Query(ctx, "EXPLAIN "+pendingPortfolioTradesSQL, tenant, ledger.ID, "2024-01-06 00:00:00")
	if e != nil {
		t.Fatal(e)
	}
	for plan.Next() {
		var line string
		if e = plan.Scan(&line); e != nil {
			t.Fatal(e)
		}
		t.Log(line)
	}
	e = plan.Err()
	plan.Close()
	if e != nil {
		t.Fatal(e)
	}
}
