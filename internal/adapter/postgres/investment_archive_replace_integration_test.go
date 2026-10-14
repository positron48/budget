package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
)

func TestInvestmentPortfolioArchiveReplacementAtomicityAndHouseholdIsolation(t *testing.T) {
	source, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, source)
	repo := NewInvestmentRepo(source)
	svc := inv.NewService(repo, NewTenantRepo(source))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledgers := []d.InvestmentLedger{}
	for i := 0; i < 8; i++ {
		l, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Account " + string(rune('A'+i)), StartedAt: at, PortfolioTracking: true}, "account-"+string(rune('a'+i)))
		if e != nil {
			t.Fatal(e)
		}
		ledgers = append(ledgers, l)
	}
	mutation := inv.Mutation{Action: "create", RequestKey: "first", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledgers[0].ID, Kind: "buy", InstrumentKey: "TEST", Currency: "RUB", Quantity: "1", Amount: "10", OccurredAt: at.Add(time.Hour)}}
	if _, e := svc.Mutate(ctx, tenant, actor, mutation); e != nil {
		t.Fatal(e)
	}
	old, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	mutation.RequestKey = "second"
	mutation.ExpectedLedgerVersion = 2
	mutation.Operation.OccurredAt = at.Add(2 * time.Hour)
	if _, e = svc.Mutate(ctx, tenant, actor, mutation); e != nil {
		t.Fatal(e)
	}
	candidate, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	target, targetDone := withPg(t)
	defer targetDone()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Target','RUB')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'replace@example.test','synthetic')`, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	targetRepo := NewInvestmentRepo(target)
	if _, e = targetRepo.RestoreInvestmentArchive(ctx, actor, old, old.SHA256); e != nil {
		t.Fatal(e)
	}
	// Actual nonempty household records must survive the investment replacement.
	var category string
	if e = target.DB.QueryRow(ctx, `INSERT INTO categories(tenant_id,kind,code) VALUES($1,'expense','household') RETURNING id`, tenant).Scan(&category); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO transactions(tenant_id,user_id,category_id,type,amount_numeric,currency_code,base_amount_numeric,base_currency_code,occurred_at) VALUES($1,$2,$3,'expense',123.45,'RUB',123.45,'RUB',$4)`, tenant, actor, category, at); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO financial_write_requests(tenant_id,operation,request_key,payload_hash,result) VALUES($1,'asset.create','household','unchanged','{"id":"household"}')`, tenant); e != nil {
		t.Fatal(e)
	}
	household := func() string {
		var raw string
		if e := target.DB.QueryRow(ctx, `SELECT jsonb_build_object('transactions',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM transactions t WHERE tenant_id=$1),'categories',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM categories c WHERE tenant_id=$1),'writes',(SELECT jsonb_agg(to_jsonb(w) ORDER BY operation,request_key) FROM financial_write_requests w WHERE tenant_id=$1 AND NOT starts_with(operation,'investment.')))::text`, tenant).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	otherTenant := uuid.NewString()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Other tenant','RUB')`, otherTenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, otherTenant, actor); e != nil {
		t.Fatal(e)
	}
	otherService := inv.NewService(NewInvestmentRepo(target), NewTenantRepo(target))
	if _, e = otherService.CreateLedgerWithDetails(ctx, otherTenant, actor, d.InvestmentLedger{Name: "Other tenant account", StartedAt: at, PortfolioTracking: true}, "other"); e != nil {
		t.Fatal(e)
	}
	otherBefore, e := targetRepo.ExportInvestmentArchive(ctx, otherTenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	otherHash, e := InvestmentArchiveStateSHA256(otherBefore)
	if e != nil {
		t.Fatal(e)
	}
	householdBefore := household()
	before, e := targetRepo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	state, e := InvestmentArchiveStateSHA256(before)
	if e != nil {
		t.Fatal(e)
	}
	pretty, e := json.MarshalIndent(before, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := archive.Decode(bytes.NewReader(pretty))
	if e != nil {
		t.Fatal(e)
	}
	prettyHash, e := InvestmentArchiveStateSHA256(decoded)
	if e != nil || prettyHash != state {
		t.Fatal("export formatting changed replacement fingerprint", e)
	}
	options := PortfolioArchiveReplacement{ExpectedStateSHA256: state, ExpectedArchiveSHA256: candidate.SHA256}
	unchanged := func() {
		t.Helper()
		b, e := targetRepo.ExportInvestmentArchive(ctx, tenant, actor)
		if e != nil {
			t.Fatal(e)
		}
		h, e := InvestmentArchiveStateSHA256(b)
		if e != nil || h != state || household() != householdBefore {
			t.Fatal("failed/dry replacement changed facts", e)
		}
	}
	checked, retry, e := targetRepo.ReplacePortfolioArchive(ctx, actor, candidate, options, false)
	if e != nil || retry || checked.Ledgers != 8 || checked.Operations != 2 {
		t.Fatal("replacement rehearsal", e)
	}
	unchanged()
	invalid := options
	invalid.ExpectedStateSHA256 = old.SHA256
	if _, _, e = targetRepo.ReplacePortfolioArchive(ctx, actor, candidate, invalid, true); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale replacement accepted", e)
	}
	unchanged()
	if _, _, e = targetRepo.ReplacePortfolioArchive(ctx, uuid.NewString(), candidate, options, true); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("foreign actor accepted", e)
	}
	unchanged()
	// Missing actor is checked after deletion, forcing a genuine rollback.
	brokenData := candidate.Data
	brokenData.Actors = append(append([]string(nil), brokenData.Actors...), uuid.NewString())
	broken, e := archive.Seal(brokenData)
	if e != nil {
		t.Fatal(e)
	}
	invalid = options
	invalid.ExpectedArchiveSHA256 = broken.SHA256
	if _, _, e = targetRepo.ReplacePortfolioArchive(ctx, actor, broken, invalid, true); e == nil {
		t.Fatal("late restoration failure accepted")
	}
	unchanged()
	var assetID string
	if e = target.DB.QueryRow(ctx, `INSERT INTO asset_accounts(tenant_id,name,kind,fixed_currency_code) VALUES($1,'Household investment','investment','RUB') RETURNING id`, tenant).Scan(&assetID); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO investment_asset_activations(tenant_id,account_id,ledger_id,actor_id,valuation_from,preview_hash,evidence,comparison) VALUES($1,$2,$3,$4,$5,'fixture','Synthetic guard','{}')`, tenant, assetID, ledgers[0].ID, actor, at); e != nil {
		t.Fatal(e)
	}
	if _, _, e = targetRepo.ReplacePortfolioArchive(ctx, actor, candidate, options, true); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("household-connected portfolio replaced", e)
	}
	unchanged()
	if _, e = target.DB.Exec(ctx, `DELETE FROM investment_asset_activations WHERE tenant_id=$1`, tenant); e != nil {
		t.Fatal(e)
	}
	var assetBefore string
	if e = target.DB.QueryRow(ctx, `SELECT to_jsonb(a)::text FROM asset_accounts a WHERE id=$1`, assetID).Scan(&assetBefore); e != nil {
		t.Fatal(e)
	}
	applied, retry, e := targetRepo.ReplacePortfolioArchive(ctx, actor, candidate, options, true)
	if e != nil || retry || applied != checked {
		t.Fatal("replacement apply", e)
	}
	if household() != householdBefore {
		t.Fatal("household data changed")
	}
	applied, retry, e = targetRepo.ReplacePortfolioArchive(ctx, actor, candidate, options, true)
	if e != nil || !retry || applied != checked {
		t.Fatal("replacement retry", e)
	}
	restarted := inv.NewService(NewInvestmentRepo(target), NewTenantRepo(target))
	accounts, e := restarted.List(ctx, tenant, actor, 200, 0)
	if e != nil || len(accounts) != 8 {
		t.Fatal("empty accounts lost", e)
	}
	snapshot, e := restarted.Snapshot(ctx, tenant, actor, ledgers[0].ID, time.Now().UTC())
	if e != nil || len(snapshot.Positions) != 1 || snapshot.Positions[0].Quantity != "2" {
		t.Fatal("replacement snapshot", e)
	}
	otherAfter, e := targetRepo.ExportInvestmentArchive(ctx, otherTenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	otherAfterHash, e := InvestmentArchiveStateSHA256(otherAfter)
	if e != nil || otherAfterHash != otherHash {
		t.Fatal("other tenant changed", e)
	}
	var assetAfter string
	if e = target.DB.QueryRow(ctx, `SELECT to_jsonb(a)::text FROM asset_accounts a WHERE id=$1`, assetID).Scan(&assetAfter); e != nil || assetAfter != assetBefore {
		t.Fatal("household investment asset changed", e)
	}
	// Inspect the exact destructive predicate without executing EXPLAIN ANALYZE.
	plan, e := target.DB.Query(ctx, `EXPLAIN DELETE FROM investment_operations WHERE tenant_id=$1`, tenant)
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
