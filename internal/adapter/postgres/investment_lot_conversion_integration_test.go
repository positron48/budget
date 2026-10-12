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
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestInvestmentHistoricalLotConversionFencesBatchAndArchive(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx := context.Background()
	tenant, user, foreign := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, e := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Lot terms','USD'),($2,'Foreign','USD')`, tenant, foreign); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'lot-terms@example.test','x')`, user); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	market := NewMarketDataRepo(pool)
	mappings := md.NewMappingService(market, NewTenantRepo(pool))
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := market.ConfigureSource(ctx, scope, "Personal", true); e != nil {
		t.Fatal(e)
	}
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "TEST", Name: "Test", ClassCode: "X", Kind: "share", Currency: "USD"}
	tx, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, scope, instrument); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	boundary := start.AddDate(4, 0, 0)
	periods := []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "USD", Unit: "currency_per_unit", LotSize: "10", From: start, To: boundary, Evidence: "Synthetic historical venue terms"}, {Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "USD", Unit: "currency_per_unit", LotSize: "100", From: boundary, Evidence: "Synthetic new venue terms"}}
	change := md.MappingChange{InstrumentKey: "stock", RequestKey: "terms-1", Reason: "Reviewed synthetic terms", Periods: periods}
	if _, e = mappings.Save(ctx, tenant, user, change); e != nil {
		t.Fatal(e)
	}
	l, e := svc.CreateLedger(ctx, tenant, user, "Broker", start, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", ExpectedLedgerVersion: 1, RequestKey: "fund", Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "deposit", Currency: "USD", Amount: "1000", OccurredAt: start}}); e != nil {
		t.Fatal(e)
	}
	state := func() string {
		t.Helper()
		var out string
		if e := pool.DB.QueryRow(ctx, `SELECT jsonb_build_object('ledger',to_jsonb(l),'ops',(SELECT count(*) FROM investment_operations WHERE tenant_id=$1),'audit',(SELECT count(*) FROM investment_operation_revisions WHERE tenant_id=$1),'requests',(SELECT count(*) FROM financial_write_requests WHERE tenant_id=$1),'jobs',(SELECT count(*) FROM investment_recalculation_jobs WHERE tenant_id=$1),'links',(SELECT count(*) FROM investment_source_links WHERE tenant_id=$1))::text FROM investment_ledgers l WHERE tenant_id=$1 AND id=$2`, tenant, l.ID).Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	before := state()
	old, e := svc.PreviewLotQuantity(ctx, tenant, user, inv.LotQuantityRequest{InstrumentKey: "stock", Lots: "2.0", OccurredAt: boundary.Add(-time.Microsecond)})
	if e != nil || old.Quantity != "20" || old.Conversion.LotSize != "10" || old.Conversion.MappingVersion != 1 {
		t.Fatal(old, e)
	}
	newer, e := svc.PreviewLotQuantity(ctx, tenant, user, inv.LotQuantityRequest{InstrumentKey: "stock", Lots: "2", Timing: &d.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: "2024-01-01"}})
	if e != nil || newer.Quantity != "200" || !newer.EffectiveAt.Equal(boundary) {
		t.Fatal(newer, e)
	}
	large, e := svc.PreviewLotQuantity(ctx, tenant, user, inv.LotQuantityRequest{InstrumentKey: "stock", Lots: "9007199254740993.123456789", OccurredAt: start})
	if e != nil || large.Quantity != "90071992547409931.23456789" {
		t.Fatal(large, e)
	}
	if _, e = svc.PreviewLotQuantity(ctx, tenant, user, inv.LotQuantityRequest{InstrumentKey: "stock", Lots: "1", OccurredAt: start.Add(-time.Microsecond)}); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("past gap used current lot", e)
	}
	if _, e = svc.PreviewLotQuantity(ctx, foreign, user, inv.LotQuantityRequest{InstrumentKey: "stock", Lots: "2", OccurredAt: start}); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("foreign lookup", e)
	}
	if state() != before {
		t.Fatal("quantity lookup wrote facts")
	}
	m := inv.Mutation{Action: "create", ExpectedLedgerVersion: 2, RequestKey: "buy", Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "buy", InstrumentKey: "stock", Currency: "USD", Amount: "20", Quantity: "0", OccurredAt: old.EffectiveAt, LotConversion: old.Conversion}}
	review, e := svc.PreviewMutation(ctx, tenant, user, m)
	if e != nil || !review.Valid || review.Proposed.Quantity != "20" {
		t.Fatal(review, e)
	}
	m.PreviewHash = review.Hash
	posted, e := svc.Mutate(ctx, tenant, user, m)
	if e != nil || posted.Quantity != "20" {
		t.Fatal(posted, e)
	}
	// Metadata changes without a journal write invalidate both manual and batch proofs.
	pending := m
	pending.RequestKey = "stale"
	pending.ExpectedLedgerVersion = 3
	pending.PreviewHash = ""
	review, e = svc.PreviewMutation(ctx, tenant, user, pending)
	if e != nil || !review.Valid {
		t.Fatal(review, e)
	}
	pending.PreviewHash = review.Hash
	prepared, e := imp.PrepareUpload(imp.Upload{Filename: "lot.csv", RequestKey: "upload", CSV: []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nBUY,2023-12-31 23:59:59,TEST,1,20,USD,0,X,0,,False,\n"), Accounts: []imp.ImportAccount{{SourceKey: "other", Name: "Other"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "other"}}})
	if e != nil {
		t.Fatal(e)
	}
	imported, e := NewInvestmentImportRepo(pool).SaveImport(ctx, tenant, user, prepared)
	if e != nil {
		t.Fatal(e)
	}
	batch := inv.ImportBatch{ImportID: imported.ID, PreviewHash: prepared.Preview.Hash, ExpectedImportVersion: 1, RequestKey: "batch", ExpectedLedgerVersions: map[string]int64{l.ID: 3}, Entries: []inv.ImportBatchEntry{{Record: 1, ExpectedRowVersion: 1, ReviewReason: "Reviewed units", Operation: m.Operation}}}
	bp, e := svc.PreviewImportBatch(ctx, tenant, user, batch)
	if e != nil {
		t.Fatal(e)
	}
	change.ExpectedVersion = 1
	change.RequestKey = "terms-2"
	change.Periods = append([]md.MappingPeriod(nil), periods...)
	change.Periods[0].LotSize = "5"
	if _, e = mappings.Save(ctx, tenant, user, change); e != nil {
		t.Fatal(e)
	}
	before = state()
	if _, e = svc.Mutate(ctx, tenant, user, pending); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale manual accepted", e)
	}
	if _, e = svc.PreviewMutation(ctx, tenant, user, pending); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale preview accepted", e)
	}
	if _, e = svc.CommitImportBatch(ctx, tenant, user, batch, bp.Hash); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale batch accepted", e)
	}
	if state() != before {
		t.Fatal("rejected writes changed state")
	}
	if again, e := svc.Mutate(ctx, tenant, user, m); e != nil || again.ID != posted.ID || again.Quantity != "20" {
		t.Fatal("posted retry checked current terms", again, e)
	}
	snap, e := svc.Snapshot(ctx, tenant, user, l.ID, boundary.Add(time.Hour))
	if e != nil || snap.Positions[0].Quantity != "20" {
		t.Fatal("metadata rewrote history", snap, e)
	}
	current, e := svc.PreviewLotQuantity(ctx, tenant, user, inv.LotQuantityRequest{InstrumentKey: "stock", Lots: "2", OccurredAt: old.EffectiveAt})
	if e != nil || current.Quantity != "10" {
		t.Fatal(current, e)
	}
	forged := *current.Conversion
	forged.LotSize = "6"
	fake := pending
	fake.Operation.LotConversion = &forged
	fake.Operation.Quantity = "0"
	fake.PreviewHash = ""
	if _, e = svc.PreviewMutation(ctx, tenant, user, fake); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("forged proof accepted", e)
	}
	// A freshly reviewed batch stores exact units and its captured terms atomically.
	batch.Entries[0].Operation.LotConversion = current.Conversion
	batch.Entries[0].Operation.Quantity = "0"
	bp, e = svc.PreviewImportBatch(ctx, tenant, user, batch)
	if e != nil || bp.Operations[0].Quantity != "10" {
		t.Fatal("fresh batch review", bp, e)
	}
	committed, e := svc.CommitImportBatch(ctx, tenant, user, batch, bp.Hash)
	if e != nil || !committed.Committed || committed.Operations[0].LotConversion.MappingVersion != 2 || committed.Operations[0].Quantity != "10" {
		t.Fatal("fresh batch commit", committed, e)
	}
	snap, e = svc.Snapshot(ctx, tenant, user, l.ID, boundary.Add(time.Hour))
	if e != nil || snap.Positions[0].Quantity != "30" {
		t.Fatal("batch units differ from preview", snap, e)
	}
	// Explicit units amendment drops the head proof, but the old proof must remain verifiable in audit.
	amendment := posted
	amendment.LotConversion = nil
	amendment.Note = "Confirmed security units independently"
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "amend", ExpectedLedgerVersion: 4, ExpectedOperationVersion: 1, RequestKey: "unit-amend", Reason: "Independent statement", Operation: amendment}); e != nil {
		t.Fatal(e)
	}
	bundle, e := repo.ExportInvestmentArchive(ctx, tenant, user)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal("old captured terms archive", e)
	}
	altered := bundle.Data
	altered.Tables = append([]archive.Table(nil), bundle.Data.Tables...)
	for i, table := range altered.Tables {
		if table.Name != "investment_market_mapping_revisions" {
			continue
		}
		altered.Tables[i].Rows = nil
		for _, raw := range table.Rows {
			var row struct {
				Version int64 `json:"version"`
			}
			if e = json.Unmarshal(raw, &row); e != nil {
				t.Fatal(e)
			}
			if row.Version != 1 {
				altered.Tables[i].Rows = append(altered.Tables[i].Rows, raw)
			}
		}
	}
	if e = archive.VerifyLotConversions(altered); e == nil {
		t.Fatal("missing historical audit proof accepted")
	}
	target, cleanup := withPg(t)
	defer cleanup()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restore','USD')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'lots-restore@example.test','x')`, user); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	if _, e = NewInvestmentRepo(target).CheckInvestmentArchiveRestore(ctx, user, bundle); e != nil {
		t.Fatal(e)
	}
	// A catalog instrument alone and a confirmed mapping without lot size are insufficient.
	change.ExpectedVersion = 2
	change.RequestKey = "terms-3"
	change.Periods[0].LotSize = ""
	if _, e = mappings.Save(ctx, tenant, user, change); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.PreviewLotQuantity(ctx, tenant, user, inv.LotQuantityRequest{InstrumentKey: "stock", Lots: "2", OccurredAt: old.EffectiveAt}); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("missing size fallback", e)
	}
	change.ExpectedVersion = 3
	change.RequestKey = "terms-4"
	change.Periods = nil
	if _, e = mappings.Save(ctx, tenant, user, change); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.PreviewLotQuantity(ctx, tenant, user, inv.LotQuantityRequest{InstrumentKey: "stock", Lots: "2", OccurredAt: old.EffectiveAt}); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("revoked mapping fallback", e)
	}
}
