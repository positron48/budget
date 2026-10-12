package postgres

import (
	"context"
	"errors"
	"fmt"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	"sync"
	"testing"
	"time"
)

func TestInvestmentLedgerLifecycle(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx := context.Background()
	var tenant, user, member, foreign string
	for _, v := range []struct {
		q string
		p *string
	}{{`INSERT INTO tenants(name,default_currency_code) VALUES('Lifecycle','RUB') RETURNING id`, &tenant}, {`INSERT INTO tenants(name,default_currency_code) VALUES('Foreign','RUB') RETURNING id`, &foreign}, {fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('lifecycle-%d@example.test','x') RETURNING id`, time.Now().UnixNano()), &user}, {fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('member-lifecycle-%d@example.test','x') RETURNING id`, time.Now().UnixNano()), &member}} {
		if e := pool.DB.QueryRow(ctx, v.q).Scan(v.p); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, tenant, user, member); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	l, e := svc.CreateLedgerWithDetails(ctx, tenant, user, d.InvestmentLedger{Name: " T IIS ", Broker: "T-Bank", AccountType: "iis", Comment: "Historic account", DefaultCurrency: "USD", StartedAt: at}, "create")
	if e != nil || l.MetadataVersion != 1 || l.Name != "T IIS" || l.DefaultCurrency != "USD" {
		t.Fatal(e, l)
	}
	m := inv.LedgerUpdate{ID: l.ID, Action: "edit", Name: "T regular", Broker: "T-Bank", AccountType: "regular", Comment: "Updated details", DefaultCurrency: "EUR", ExpectedMetadataVersion: 1, Reason: "Statement reviewed", RequestKey: "edit"}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := svc.UpdateLedger(ctx, tenant, user, m); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal("retry race", e)
		}
	}
	h, e := svc.LedgerHistory(ctx, tenant, member, inv.LedgerHistoryQuery{ID: l.ID, Limit: 1})
	if e != nil || h.NextAfterVersion != 1 || h.Ledger.MetadataVersion != 2 || h.Ledger.Version != 1 {
		t.Fatal("metadata independent", e, h)
	}
	h, e = svc.LedgerHistory(ctx, tenant, user, inv.LedgerHistoryQuery{ID: l.ID, AfterVersion: 1, ExpectedMetadataVersion: 2})
	if e != nil || h.Revisions[0].Before.AccountType != "iis" || h.Revisions[0].After.AccountType != "regular" || h.Revisions[0].Reason != m.Reason {
		t.Fatal("before after", e, h)
	}
	if _, e = svc.UpdateLedger(ctx, tenant, member, m); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("member write", e)
	}
	m.RequestKey = "stale"
	if _, e = svc.UpdateLedger(ctx, tenant, user, m); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale details", e)
	}
	m.Action = "archive"
	m.ExpectedMetadataVersion = 2
	m.ExpectedLedgerVersion = 1
	m.RequestKey = "archive"
	op := d.InvestmentOperation{LedgerID: l.ID, Kind: "deposit", Currency: "USD", Amount: "0.000000000001", OccurredAt: at}
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", Operation: op, ExpectedLedgerVersion: 1, RequestKey: "deposit"}); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.UpdateLedger(ctx, tenant, user, m); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale financial closure", e)
	}
	m.ExpectedLedgerVersion = 2
	if _, e = svc.UpdateLedger(ctx, tenant, user, m); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("tiny cash closure", e)
	}
	op.Kind = "withdrawal"
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", Operation: op, ExpectedLedgerVersion: 2, RequestKey: "withdraw"}); e != nil {
		t.Fatal(e)
	}
	m.ExpectedLedgerVersion = 3
	l, e = svc.UpdateLedger(ctx, tenant, user, m)
	if e != nil || !l.Archived || l.MetadataVersion != 3 {
		t.Fatal("zero archive", e, l)
	}
	list, e := svc.ListWithArchive(ctx, tenant, user, 50, 0, false)
	if e != nil || len(list) != 0 {
		t.Fatal("default list", e, list)
	}
	list, e = svc.ListWithArchive(ctx, tenant, user, 50, 0, true)
	if e != nil || len(list) != 1 {
		t.Fatal("archive list", e)
	}
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", Operation: op, ExpectedLedgerVersion: 3, RequestKey: "closed-write"}); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("closed write", e)
	}
	snap, e := svc.Snapshot(ctx, tenant, user, l.ID, at)
	if e != nil || snap.Ledger.ID != l.ID {
		t.Fatal("historical read", e)
	}
	if _, e = svc.LedgerHistory(ctx, foreign, user, inv.LedgerHistoryQuery{ID: l.ID}); e == nil {
		t.Fatal("foreign read")
	}
	bundle, e := repo.ExportInvestmentArchive(ctx, tenant, user)
	if e != nil {
		t.Fatal("export", e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal("verify", e)
	}
	// Whole-tenant restore must preserve the closed account, details, and revisions.
	for _, name := range []string{"financial_write_requests", "investment_recalculation_jobs", "investment_cash_legs", "investment_security_legs", "investment_operation_revisions", "investment_operations", "investment_ledger_revisions", "investment_ledgers"} {
		if _, e = pool.DB.Exec(ctx, "DELETE FROM "+name+" WHERE tenant_id=$1", tenant); e != nil {
			t.Fatal(name, e)
		}
	}
	if _, e = repo.RestoreInvestmentArchive(ctx, user, bundle, bundle.SHA256); e != nil {
		t.Fatal("restore archive", e)
	}
	h, e = svc.LedgerHistory(ctx, tenant, user, inv.LedgerHistoryQuery{ID: l.ID})
	if e != nil || len(h.Revisions) != 3 || !h.Ledger.Archived || h.Ledger.DefaultCurrency != "EUR" {
		t.Fatal("restored metadata", e, h)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0051_investment_ledger_lifecycle")
	m.Action = "restore"
	m.ExpectedMetadataVersion = 3
	m.RequestKey = "restore"
	l, e = svc.UpdateLedger(ctx, tenant, user, m)
	if e != nil || l.Archived {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "deposit", Currency: "EUR", Amount: "1", OccurredAt: at}, ExpectedLedgerVersion: 3, RequestKey: "restored-write"}); e != nil {
		t.Fatal(e)
	}
}
