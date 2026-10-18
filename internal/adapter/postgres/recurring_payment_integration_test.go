package postgres

import (
	"context"
	"errors"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/asset"
	rp "github.com/positron48/budget/internal/usecase/recurringpayment"
	"github.com/positron48/budget/internal/usecase/transaction"
	"github.com/positron48/budget/migrations"
	"sync"
	"testing"
	"time"
)

func paymentPtr(n int64) *int64 { return &n }
func TestRecurringPaymentsAccounting_PG(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	var tenant, user, cat, foreign string
	must(pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Payments','RUB') RETURNING id`).Scan(&tenant))
	must(pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Foreign','RUB') RETURNING id`).Scan(&foreign))
	must(pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('payments@example.test','test') RETURNING id`).Scan(&user))
	must(pool.DB.QueryRow(ctx, `INSERT INTO categories(tenant_id,kind,code) VALUES($1,'expense','rent') RETURNING id`, tenant).Scan(&cat))
	ctx = ctxutil.WithUserID(ctxutil.WithTenantID(ctx, tenant), user)
	repo := NewRecurringPaymentRepo(pool)
	txs := transaction.NewService(NewTransactionRepo(pool), NewFxRepo(pool), NewTenantRepo(pool), NewCategoryRepo(pool))
	svc := rp.New(repo, txs, NewFxRepo(pool))
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }
	result, e := svc.ChangeTemplate(ctx, tenant, user, rp.TemplateChange{Template: domain.PaymentTemplate{Name: "Utilities", CategoryID: cat, CurrencyCode: "RUB", Timezone: "Europe/Moscow", AmountMode: "variable", InitialEstimate: paymentPtr(800000), FirstMonth: "2026-09", IntervalMonths: 1, Day: 10, PeriodOffset: -1}, RequestKey: "create"})
	must(e)
	all, e := svc.Read(ctx, tenant, "2026-10", "RUB")
	must(e)
	if len(all) != 14 {
		t.Fatalf("horizon %d", len(all))
	}
	var sept, oct domain.PaymentOccurrence
	for _, o := range all {
		if o.ScheduledMonth == "2026-09" {
			sept = o
		}
		if o.ScheduledMonth == "2026-10" {
			oct = o
		}
	}
	if oct.PeriodMonth != "2026-09" || oct.Status != "overdue" {
		t.Fatal(oct)
	}
	assetSvc := asset.NewService(NewAssetRepo(pool), NewFxRepo(pool), NewTenantRepo(pool))
	bank, e := assetSvc.CreateAccount(ctx, domain.AssetAccount{TenantID: tenant, Name: "Bank", Kind: "bank"}, []domain.AssetSnapshot{{TenantID: tenant, UserID: user, Amount: domain.Money{CurrencyCode: "RUB", MinorUnits: 2000000}, AsOf: now.Add(-48 * time.Hour)}}, "bank")
	must(e)
	confirm := rp.Confirmation{ID: oct.ID, Version: oct.Version, RequestKey: "pay", AccountID: bank.ID, Amount: 845000, CurrencyCode: "RUB", OccurredAt: now.Add(-time.Hour)}
	first, e := svc.Confirm(ctx, tenant, user, confirm)
	must(e)
	again, e := svc.Confirm(ctx, tenant, user, confirm)
	must(e)
	if first.Transaction.ID != again.Transaction.ID {
		t.Fatal("duplicate")
	}
	bankAfter, e := NewAssetRepo(pool).GetAccount(ctx, tenant, bank.ID, now)
	must(e)
	if len(bankAfter.Balances) != 1 || bankAfter.Balances[0].Amount.MinorUnits != 1155000 {
		t.Fatal("duplicate or missing ledger movement", bankAfter.Balances)
	}
	confirm.Amount++
	if _, e = svc.Confirm(ctx, tenant, user, confirm); !errors.Is(e, domain.ErrIdempotencyConflict) {
		t.Fatal(e)
	}
	all, e = svc.Read(ctx, tenant, "2026-11", "RUB")
	must(e)
	for _, o := range all {
		if o.ScheduledMonth == "2026-09" && *o.ExpectedAmount != 800000 {
			t.Fatal("future payment used as estimate")
		}
		if o.ScheduledMonth == "2026-11" && *o.ExpectedAmount != 845000 {
			t.Fatal("estimate not refreshed")
		}
	}
	if _, e = svc.Change(ctx, tenant, user, rp.Change{ID: first.ID, Version: first.Version, RequestKey: "skip-paid", Action: "skip", Skipped: true}); !errors.Is(e, rp.ErrPrecondition) {
		t.Fatal("skipped linked expense", e)
	}
	if _, e = svc.Confirm(ctx, tenant, user, rp.Confirmation{ID: sept.ID, Version: sept.Version, RequestKey: "used-expense", TransactionID: first.Transaction.ID, Amount: first.Transaction.Amount.MinorUnits, CurrencyCode: "RUB", OccurredAt: first.Transaction.OccurredAt}); !errors.Is(e, rp.ErrPrecondition) {
		t.Fatal("expense linked twice", e)
	}
	// Concurrent different keys still create one expense.
	var wg sync.WaitGroup
	success := 0
	var mu sync.Mutex
	for _, key := range []string{"race-a", "race-b", "race-c", "race-d", "race-e", "race-f", "race-g", "race-h"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			_, e := svc.Confirm(ctx, tenant, user, rp.Confirmation{ID: sept.ID, Version: sept.Version, RequestKey: key, Amount: 700000, CurrencyCode: "RUB", OccurredAt: now.Add(-2 * time.Hour)})
			if e == nil {
				mu.Lock()
				success++
				mu.Unlock()
			} else if !errors.Is(e, rp.ErrConflict) {
				t.Error(e)
			}
		}(key)
	}
	wg.Wait()
	if success != 1 {
		t.Fatal("race", success)
	}
	// Original expense updates invalidate confirmation; reconfirm and unlink keep the expense.
	tx := *first.Transaction
	tx.Amount.MinorUnits = 850000
	_, e = txs.Update(ctx, tx)
	must(e)
	all, e = svc.Read(ctx, tenant, "2026-10", "RUB")
	must(e)
	for _, o := range all {
		if o.ID == oct.ID && o.Status != "needs_review" {
			t.Fatal("changed expense stays paid")
		}
	}
	if _, e = svc.Confirm(ctx, tenant, user, rp.Confirmation{ID: first.ID, Version: first.Version, RequestKey: "no-accept", TransactionID: tx.ID, Amount: 850000, CurrencyCode: "RUB", OccurredAt: tx.OccurredAt}); !errors.Is(e, rp.ErrPrecondition) {
		t.Fatal("silently changed exact amount", e)
	}
	_, e = svc.Confirm(ctx, tenant, user, rp.Confirmation{ID: first.ID, Version: first.Version, RequestKey: "reconfirm", TransactionID: tx.ID, Amount: 850000, CurrencyCode: "RUB", OccurredAt: tx.OccurredAt, AcceptAmountChange: true})
	must(e)
	current, e := repo.get(ctx, tenant, oct.ID, false)
	must(e)
	_, e = svc.Change(ctx, tenant, user, rp.Change{ID: oct.ID, Version: current.Version, RequestKey: "unlink", Action: "unlink"})
	must(e)
	_, e = txs.Get(ctx, tx.ID)
	must(e)
	current, e = repo.get(ctx, tenant, oct.ID, false)
	must(e)
	_, e = svc.Confirm(ctx, tenant, user, rp.Confirmation{ID: oct.ID, Version: current.Version, RequestKey: "relink", TransactionID: tx.ID, Amount: 850000, CurrencyCode: "RUB", OccurredAt: tx.OccurredAt})
	must(e)
	must(txs.Delete(ctx, tx.ID))
	current, e = repo.get(ctx, tenant, oct.ID, false)
	must(e)
	if current.Transaction != nil {
		t.Fatal("delete retained link")
	}
	bankAfter, e = NewAssetRepo(pool).GetAccount(ctx, tenant, bank.ID, now)
	must(e)
	if bankAfter.Balances[0].Amount.MinorUnits != 2000000 {
		t.Fatal("deleted expense retained movement")
	}
	all, e = svc.Read(ctx, tenant, "2026-11", "RUB")
	must(e)
	for _, o := range all {
		if o.ScheduledMonth == "2026-11" && *o.ExpectedAmount != 700000 {
			t.Fatal("deleted payment retained estimate")
		}
	}
	// Failed account creation rolls the entire expense back.
	var before, after int
	must(pool.DB.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE tenant_id=$1`, tenant).Scan(&before))
	_, e = svc.Confirm(ctx, tenant, user, rp.Confirmation{ID: oct.ID, Version: current.Version, RequestKey: "bad-account", Amount: 850000, CurrencyCode: "RUB", AccountID: "00000000-0000-0000-0000-000000000099", OccurredAt: now})
	if e == nil {
		t.Fatal("invalid account accepted")
	}
	must(pool.DB.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE tenant_id=$1`, tenant).Scan(&after))
	if before != after {
		t.Fatal("rollback leaked expense")
	}
	foreignCtx := ctxutil.WithTenantID(ctx, foreign)
	if _, e = repo.get(foreignCtx, foreign, oct.ID, true); !errors.Is(e, rp.ErrNotFound) {
		t.Fatal("cross tenant", e)
	}
	// Manual future exception survives edits and archive. Past snapshots do not change.
	var future domain.PaymentOccurrence
	all, e = svc.Read(ctx, tenant, "2026-12", "RUB")
	must(e)
	for _, o := range all {
		if o.ScheduledMonth == "2026-12" {
			future = o
		}
	}
	_, e = svc.Change(ctx, tenant, user, rp.Change{ID: future.ID, Version: future.Version, RequestKey: "move", Action: "update", DueDate: "2027-01-05", ExactAmount: paymentPtr(123), Note: "exception"})
	must(e)
	template := result.Template
	template.Day = 31
	template.IntervalMonths = 3
	template.PeriodOffset = 0
	template.FirstMonth = "2026-10"
	template.Name = "Revised"
	result, e = svc.ChangeTemplate(ctx, tenant, user, rp.TemplateChange{Template: template, RequestKey: "edit"})
	must(e)
	all, e = svc.Read(ctx, tenant, "2027-06", "RUB")
	must(e)
	for _, o := range all {
		if o.ScheduledMonth < "2026-11" && o.Name != "Utilities" {
			t.Fatal("history rewritten")
		}
		if o.ID == future.ID && o.DueDate != "2027-01-05" {
			t.Fatal("exception lost")
		}
	}
	_, e = svc.ChangeTemplate(ctx, tenant, user, rp.TemplateChange{Template: result.Template, RequestKey: "archive", Archive: true})
	must(e)
	all, e = svc.Read(ctx, tenant, "2028-10", "RUB")
	must(e)
	if len(all) != 3 {
		t.Fatalf("archive retained untouched future plans: %d", len(all))
	}
	// Plans do not alter actual totals; paid FX uses the historical snapshot.
	_, beforeExpense, _, e := NewTransactionRepo(pool).Totals(ctx, tenant, transaction.ListFilter{})
	must(e)
	dollar, e := svc.ChangeTemplate(ctx, tenant, user, rp.TemplateChange{Template: domain.PaymentTemplate{Name: "Dollar subscription", CategoryID: cat, CurrencyCode: "USD", Timezone: "Europe/Madrid", AmountMode: "fixed", FixedAmount: paymentPtr(1000), FirstMonth: "2026-10", Day: 31, IntervalMonths: 1}, RequestKey: "dollar"})
	must(e)
	all, e = svc.Read(ctx, tenant, "2026-11", "RUB")
	must(e)
	var dollarOctober domain.PaymentOccurrence
	for _, o := range all {
		if o.TemplateID == dollar.Template.ID && o.ScheduledMonth == "2026-10" {
			dollarOctober = o
			if !o.Incomplete || o.DisplayAmount != nil {
				t.Fatal("missing FX silently zeroed", o)
			}
		}
	}
	_, expense, _, e := NewTransactionRepo(pool).Totals(ctx, tenant, transaction.ListFilter{})
	must(e)
	if expense != beforeExpense {
		t.Fatal("plan entered reports")
	}
	_, e = NewFxRepo(pool).UpsertRate(ctx, "USD", "RUB", "90", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "manual")
	must(e)
	_, e = NewFxRepo(pool).UpsertRate(ctx, "USD", "RUB", "100", time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC), "manual")
	must(e)
	_, e = svc.Confirm(ctx, tenant, user, rp.Confirmation{ID: dollarOctober.ID, Version: dollarOctober.Version, RequestKey: "dollar-pay", Amount: 1000, CurrencyCode: "USD", OccurredAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)})
	must(e)
	all, e = svc.Read(ctx, tenant, "2026-11", "RUB")
	must(e)
	for _, o := range all {
		if o.TemplateID != dollar.Template.ID {
			continue
		}
		if o.ScheduledMonth == "2026-10" && (o.Status != "paid" || o.DisplayAmount.MinorUnits != 90000) {
			t.Fatal("historical FX", o)
		}
		if o.ScheduledMonth == "2026-11" && (o.DisplayAmount.MinorUnits != 100000 || o.FXDate != "2026-10-18") {
			t.Fatal("current FX", o)
		}
	}
	_, expense, _, e = NewTransactionRepo(pool).Totals(ctx, tenant, transaction.ListFilter{})
	must(e)
	if expense-beforeExpense != 90000 {
		t.Fatal("actual historical expense", expense-beforeExpense)
	}

	down, e := migrations.FS.ReadFile("0064_recurring_payments.down.sql")
	must(e)
	if _, e = pool.DB.Exec(ctx, string(down)); e == nil {
		t.Fatal("down migration erased user data")
	}

}
