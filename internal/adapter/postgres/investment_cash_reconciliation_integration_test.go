package postgres

import (
	"context"
	"errors"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	"testing"
	"time"
)

func TestInvestmentCashReconciliationPreviewAuditAndReplay(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx := context.Background()
	var tenant, user string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Reconciliation','USD') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('cash-reconciliation@example.test','x') RETURNING id`).Scan(&user); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	l, e := svc.CreateLedger(ctx, tenant, user, "Broker", at, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	fund, e := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "fund", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "deposit", Currency: "USD", Amount: "100.123456789012", OccurredAt: at}})
	if e != nil {
		t.Fatal(e)
	}
	m := inv.Mutation{Action: "create", RequestKey: "observe", ExpectedLedgerVersion: 2, Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "cash_reconciliation", Currency: "USD", OccurredAt: at.Add(time.Hour), Note: "Statement 2024-01-01, settled USD", CashReconciliation: &d.InvestmentCashReconciliation{ExpectedSettled: fund.Amount, ObservedSettled: "99"}}}
	check, e := svc.PreviewMutation(ctx, tenant, user, m)
	if e != nil || !check.Valid || check.Comparisons[0].CashChange[0].Settled != "-1.123456789012" {
		t.Fatal(check, e)
	}
	m.PreviewHash = check.Hash
	posted, e := svc.Mutate(ctx, tenant, user, m)
	if e != nil || posted.Amount != "1.123456789012" || posted.CashReconciliation.ObservedSettled != "99" {
		t.Fatal(posted, e)
	}
	again, e := svc.Mutate(ctx, tenant, user, m)
	if e != nil || again.ID != posted.ID {
		t.Fatal("retry", again, e)
	}
	var leg string
	if e = pool.DB.QueryRow(ctx, `SELECT amount::text FROM investment_cash_legs WHERE operation_id=$1`, posted.ID).Scan(&leg); e != nil || leg != "-1.123456789012" {
		t.Fatal(leg, e)
	}
	snap, e := svc.Snapshot(ctx, tenant, user, l.ID, at.Add(2*time.Hour))
	if e != nil || snap.Cash[0].Settled != "99" {
		t.Fatal(snap, e)
	}
	// An edit before this reviewed observation must not silently alter its delta.
	fund.Amount = "101.123456789012"
	_, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "amend", RequestKey: "amend-fund", Reason: "Correct statement", ExpectedLedgerVersion: 3, ExpectedOperationVersion: fund.Version, Operation: fund})
	if !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("history edit bypassed observation", e)
	}
	var version int64
	if e = pool.DB.QueryRow(ctx, `SELECT version FROM investment_ledgers WHERE id=$1`, l.ID).Scan(&version); e != nil || version != 3 {
		t.Fatal("failed edit wrote version", version, e)
	}
	// Correction remains explicit in revisions and invalidates derived series.
	revised := posted
	revised.CashReconciliation = &d.InvestmentCashReconciliation{ExpectedSettled: "100.123456789012", ObservedSettled: "98"}
	revised.Amount = "0"
	amended, e := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "amend", RequestKey: "fix-observation", Reason: "Correct statement page", ExpectedLedgerVersion: 3, ExpectedOperationVersion: posted.Version, Operation: revised})
	if e != nil || amended.Version != 2 {
		t.Fatal(amended, e)
	}
	var n int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operation_revisions WHERE operation_id=$1`, posted.ID).Scan(&n); e != nil || n != 2 {
		t.Fatal("missing before/after audit", n, e)
	}
	bundle, e := repo.ExportInvestmentArchive(ctx, tenant, user)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal("reconciliation archive must independently replay", e)
	}
	target, cleanup := withPg(t)
	defer cleanup()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restore','USD')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'restore-reconciliation@example.test','x')`, user); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	if _, e = NewInvestmentRepo(target).CheckInvestmentArchiveRestore(ctx, user, bundle); e != nil {
		t.Fatal("restore rehearsal", e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0052_investment_cash_reconciliation")
	// Void reverses only the observation, preserving the original funding.
	_, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "void", RequestKey: "void-observation", Reason: "Use full statement instead", ExpectedLedgerVersion: 4, ExpectedOperationVersion: amended.Version, Operation: amended})
	if e != nil {
		t.Fatal(e)
	}
	snap, e = svc.Snapshot(ctx, tenant, user, l.ID, at.Add(2*time.Hour))
	if e != nil || snap.Cash[0].Settled != "100.123456789012" {
		t.Fatal(snap, e)
	}
}
