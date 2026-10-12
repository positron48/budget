package postgres

import (
	"context"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	"testing"
	"time"
)

func TestInvestmentDateOnlyPreviewSettlementAuditAndArchive(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx := context.Background()
	var tenant, user string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Timing','USD') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('timing@example.test','x') RETURNING id`).Scan(&user); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	start := time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC)
	l, e := svc.CreateLedger(ctx, tenant, user, "Broker", start, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	_, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", ExpectedLedgerVersion: 1, RequestKey: "fund", Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "deposit", Currency: "USD", Amount: "100", Timing: &d.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-01"}}})
	if e != nil {
		t.Fatal(e)
	}
	m := inv.Mutation{Action: "create", ExpectedLedgerVersion: 2, RequestKey: "buy", Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "buy", InstrumentKey: "stock", Currency: "USD", Quantity: "1", Amount: "30", Timing: &d.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-02", SettlementDate: "2024-01-03", SourceDate: "2024-01-02"}}}
	p, e := svc.PreviewMutation(ctx, tenant, user, m)
	if e != nil || !p.Valid {
		t.Fatal(p, e)
	}
	m.PreviewHash = p.Hash
	buy, e := svc.Mutate(ctx, tenant, user, m)
	if e != nil || buy.OccurredAt.Format(time.RFC3339) != "2024-01-01T21:00:00Z" || buy.SettlementAt.Format(time.RFC3339Nano) != "2024-01-03T20:59:59.999999Z" {
		t.Fatal(buy, e)
	}
	for _, tc := range []struct {
		at               time.Time
		settled, payable string
	}{{buy.OccurredAt, "100", "30"}, {buy.SettlementAt.Add(-time.Microsecond), "100", "30"}, {buy.SettlementAt, "70", "0"}} {
		s, e := svc.Snapshot(ctx, tenant, user, l.ID, tc.at)
		if e != nil || s.Cash[0].Settled != tc.settled || s.Cash[0].Payable != tc.payable {
			t.Fatal(s, e)
		}
	}
	again, e := svc.Mutate(ctx, tenant, user, m)
	if e != nil || again.ID != buy.ID {
		t.Fatal(again, e)
	}
	// Upgrade occurrence precision from the broker evidence; retain source civil date.
	changed := buy
	changed.Timing = &d.InvestmentOperationTiming{Timezone: "Europe/Moscow", SourceDate: "2024-01-02", SettlementDate: "2024-01-03"}
	changed.OccurredAt = buy.OccurredAt.Add(12 * time.Hour)
	amended, e := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "amend", ExpectedLedgerVersion: 3, ExpectedOperationVersion: buy.Version, RequestKey: "precise", Reason: "Broker execution timestamp", Operation: changed})
	if e != nil || amended.Timing.OccurredDate != "" || amended.Timing.SourceDate != "2024-01-02" {
		t.Fatal(amended, e)
	}
	var original string
	if e = pool.DB.QueryRow(ctx, `SELECT after_data->'Timing'->>'OccurredDate' FROM investment_operation_revisions WHERE operation_id=$1 AND version=1`, buy.ID).Scan(&original); e != nil || original != "2024-01-02" {
		t.Fatal("date-only provenance lost", original, e)
	}
	bundle, e := repo.ExportInvestmentArchive(ctx, tenant, user)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal(e)
	}
	target, cleanup := withPg(t)
	defer cleanup()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restore','USD')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'timing-restore@example.test','x')`, user); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	if _, e = NewInvestmentRepo(target).CheckInvestmentArchiveRestore(ctx, user, bundle); e != nil {
		t.Fatal(e)
	}
	// A confirmed date-only cash observation today is valid even though its
	// conditional application point is day-end, rather than a future real event.
	today := time.Now().UTC().Format(time.DateOnly)
	cash, e := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", ExpectedLedgerVersion: 4, RequestKey: "today", Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "deposit", Currency: "USD", Amount: "5", Timing: &d.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: today}}})
	if e != nil || cash.OccurredAt.Format("2006-01-02T15:04:05.999999") != today+"T23:59:59.999999" {
		t.Fatal("today's observation rejected", cash, e)
	}
	endSnapshot, e := svc.Snapshot(ctx, tenant, user, l.ID, cash.OccurredAt)
	if e != nil || endSnapshot.Cash[0].Settled != "75" {
		t.Fatal(endSnapshot, e)
	}
}
