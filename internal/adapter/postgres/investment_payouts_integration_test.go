package postgres

import (
	"context"
	"errors"
	"fmt"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	"testing"
	"time"
)

func TestInvestmentPayoutsPersistedFactsAndIsolation(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, foreign, user, outsider string
	for _, f := range []struct {
		sql string
		out *string
	}{{`INSERT INTO tenants(name,default_currency_code) VALUES('Payouts','USD') RETURNING id`, &tenant}, {`INSERT INTO tenants(name,default_currency_code) VALUES('Foreign','USD') RETURNING id`, &foreign}, {fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('payouts-%d@example.test','fixture') RETURNING id`, time.Now().UnixNano()), &user}, {fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('outsider-%d@example.test','fixture') RETURNING id`, time.Now().UnixNano()), &outsider}} {
		if e := pool.DB.QueryRow(ctx, f.sql).Scan(f.out); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner'),($3,$2,'owner')`, tenant, user, foreign); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Date(2024, 1, 31, 22, 0, 0, 0, time.UTC)
	l, e := svc.CreateLedger(ctx, tenant, user, "Broker", at.AddDate(0, -1, 0), "ledger")
	if e != nil {
		t.Fatal(e)
	}
	foreignLedger, e := svc.CreateLedger(ctx, foreign, user, "Foreign", at, "foreign")
	if e != nil {
		t.Fatal(e)
	}
	version := int64(1)
	create := func(kind, amount, key string) d.InvestmentOperation {
		t.Helper()
		o := d.InvestmentOperation{LedgerID: l.ID, Kind: kind, Amount: amount, Currency: "USD", OccurredAt: at}
		if kind == "dividend" {
			o.InstrumentKey = "STOCK"
			o.Tax = "0.1"
		}
		saved, e := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: key, ExpectedLedgerVersion: version, Operation: o})
		if e != nil {
			t.Fatal(kind, e)
		}
		version++
		return saved
	}
	create("deposit", "1000", "deposit")
	dividend := create("dividend", "10.1234567891", "dividend")
	create("fee", "1", "fee")
	create("tax", "2", "tax")
	create("tax_refund", "3", "refund")
	q := inv.PayoutQuery{FromDate: "2024-02-01", ToDate: "2024-02-29", Limit: 1}
	report, e := svc.Payouts(ctx, tenant, user, q)
	if e != nil || report.TotalCount != 4 || len(report.Entries) != 1 || report.NextCursor == "" || len(report.Totals) != 1 || len(report.Months) != 1 {
		t.Fatal("report", e, report)
	}
	if report.Totals[0].NetCash.RatString() != "100234567891/10000000000" || report.Totals[0].IncomeNet.RatString() != "100234567891/10000000000" {
		t.Fatal("exact totals", report.Totals)
	}
	if len(report.Growth) != 1 || report.Growth[0].Status != "incomplete_history" || report.Growth[0].PercentChange != nil {
		t.Fatal("missing baseline certified", report.Growth)
	}
	// The recorded refund exactly offsets standalone fees/tax in this fixture.
	seen := map[string]bool{}
	for {
		for _, entry := range report.Entries {
			if seen[entry.ID] {
				t.Fatal("duplicate", entry.ID)
			}
			seen[entry.ID] = true
		}
		if report.NextCursor == "" {
			break
		}
		q.Cursor = report.NextCursor
		report, e = svc.Payouts(ctx, tenant, user, q)
		if e != nil {
			t.Fatal(e)
		}
	}
	q.Cursor = ""
	first, e := svc.Payouts(ctx, tenant, user, q)
	if e != nil {
		t.Fatal(e)
	}
	q.Cursor = first.NextCursor
	dividend.Amount = "20.1234567891"
	saved, e := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "amend", RequestKey: "amend", Reason: "Statement correction", ExpectedLedgerVersion: version, ExpectedOperationVersion: dividend.Version, Operation: dividend})
	if e != nil {
		t.Fatal(e)
	}
	version++
	if _, e = svc.Payouts(ctx, tenant, user, q); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("amended cursor accepted", e)
	}
	q.Cursor = ""
	q.Kinds = []string{"dividend"}
	report, e = svc.Payouts(ctx, tenant, user, q)
	if e != nil || report.TotalCount != 1 || report.Entries[0].OperationVersion != saved.Version || report.Entries[0].Gross.Cmp(report.Totals[0].Gross) != 0 {
		t.Fatal("current correction", e, report)
	}
	_, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "void", RequestKey: "void", Reason: "Duplicate payment", ExpectedLedgerVersion: version, ExpectedOperationVersion: saved.Version, Operation: saved})
	if e != nil {
		t.Fatal(e)
	}
	report, e = svc.Payouts(ctx, tenant, user, q)
	if e != nil || report.TotalCount != 0 || len(report.Totals) != 0 {
		t.Fatal("void still counted", e)
	}
	q.Kinds = nil
	q.LedgerIDs = []string{foreignLedger.ID}
	if _, e = svc.Payouts(ctx, tenant, user, q); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign ledger leaked", e)
	}
	q.LedgerIDs = nil
	if _, e = repo.Payouts(ctx, tenant, outsider, q); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("repository membership bypass", e)
	}
	bundle, e := repo.ExportInvestmentArchive(ctx, tenant, user)
	if e != nil {
		t.Fatal("refund canonical archive", e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal("refund independent archive replay", e)
	}

	assertInvestmentRollbackRefused(t, ctx, pool, "0050_investment_tax_refund")
	// A refund is positive settled cash and a positive tax component, not an external contribution.
	snapshot, e := svc.Snapshot(ctx, tenant, user, l.ID, at.Add(time.Hour))
	if e != nil || snapshot.Cash[0].Settled != "1000" {
		t.Fatal("refund replay", e, snapshot)
	}
}

func TestInvestmentPayoutGrowthPersistedBaselineAmendAndVoid(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, user string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Growth','USD') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id`, fmt.Sprintf("growth-%d@example.test", time.Now().UnixNano())).Scan(&user); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Date(2023, 1, 31, 22, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, user, "Growth", at.AddDate(-1, 0, 0), "growth-ledger")
	if e != nil {
		t.Fatal(e)
	}
	old, e := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "old", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "dividend", InstrumentKey: "STOCK", Currency: "USD", Amount: "10", Tax: "1", OccurredAt: at}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "new", ExpectedLedgerVersion: 2, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "dividend", InstrumentKey: "STOCK", Currency: "USD", Amount: "20", Tax: "2", OccurredAt: at.AddDate(1, 0, 0)}})
	if e != nil {
		t.Fatal(e)
	}
	q := inv.PayoutQuery{FromDate: "2024-02-01", ToDate: "2024-02-29", Limit: 1}
	check := func(status, previous, percent string) {
		t.Helper()
		r, e := svc.Payouts(ctx, tenant, user, q)
		if e != nil || len(r.Growth) != 1 || r.Growth[0].Status != status || r.Growth[0].PreviousIncomeNet.RatString() != previous {
			t.Fatal(e, r.Growth)
		}
		if percent != "" && (r.Growth[0].PercentChange == nil || r.Growth[0].PercentChange.RatString() != percent) {
			t.Fatal(r.Growth)
		}
		if percent == "" && r.Growth[0].PercentChange != nil {
			t.Fatal("undefined percent", r.Growth)
		}
	}
	check("available", "9", "100")
	old.Amount = "15"
	saved, e := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "amend", RequestKey: "amend-old", Reason: "Historical correction", ExpectedLedgerVersion: 3, ExpectedOperationVersion: old.Version, Operation: old})
	if e != nil {
		t.Fatal(e)
	}
	check("available", "14", "200/7")
	_, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "void", RequestKey: "void-old", Reason: "Historical duplicate", ExpectedLedgerVersion: 4, ExpectedOperationVersion: saved.Version, Operation: saved})
	if e != nil {
		t.Fatal(e)
	}
	check("zero_baseline", "0", "")
}
