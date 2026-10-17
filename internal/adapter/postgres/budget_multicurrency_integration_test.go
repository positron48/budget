package postgres

import (
	"context"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/fximport"
	"github.com/positron48/budget/internal/usecase/report"
	txuse "github.com/positron48/budget/internal/usecase/transaction"
	"testing"
	"time"
)

func TestBudgetMulticurrency_PG(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var tenant, user, cat string
	must(pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Multi','RUB') RETURNING id`).Scan(&tenant))
	must(pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('multi@example.com','test') RETURNING id`).Scan(&user))
	must(pool.DB.QueryRow(ctx, `INSERT INTO categories(tenant_id,kind,code) VALUES($1,'expense','food') RETURNING id`, tenant).Scan(&cat))
	ctx = ctxutil.WithUserID(ctxutil.WithTenantID(ctx, tenant), user)
	fx := NewFxRepo(pool)
	friday := time.Date(2019, 10, 4, 0, 0, 0, 0, time.UTC)
	sunday := friday.AddDate(0, 0, 2)
	must(fx.SaveCBRRates(ctx, []fximport.Quote{
		{Currency: "EUR", Rate: "125", Date: friday}, {Currency: "USD", Rate: "100", Date: friday},
		{Currency: "EUR", Rate: "250", Date: friday.AddDate(0, 0, 3)}, {Currency: "USD", Rate: "100", Date: friday.AddDate(0, 0, 3)},
	}))
	svc := txuse.NewService(NewTransactionRepo(pool), fx, NewTenantRepo(pool), NewCategoryRepo(pool))
	for _, amount := range []domain.Money{{CurrencyCode: "EUR", MinorUnits: 2000}, {CurrencyCode: "USD", MinorUnits: 1000}, {CurrencyCode: "RUB", MinorUnits: 100000}} {
		_, err := svc.CreateForUser(ctx, tenant, user, domain.TransactionTypeExpense, cat, amount, sunday, "Spain", false)
		must(err)
	}
	start, err := fx.BudgetHistoryStart(ctx)
	must(err)
	if !start.Equal(sunday) {
		t.Fatal(start)
	}
	filter := txuse.ListFilter{TargetCurrencyCode: "EUR", Page: 1, PageSize: 50}
	list, _, err := svc.List(ctx, tenant, filter)
	must(err)
	sum := int64(0)
	for _, tx := range list {
		sum += tx.DisplayAmount.MinorUnits
	}
	_, total, err := svc.Totals(ctx, tenant, filter)
	must(err)
	if sum != 3600 || total.MinorUnits != 3600 {
		t.Fatalf("list %d totals %+v", sum, total)
	}
	rep := report.NewService(svc, fx, NewTenantRepo(pool), NewCategoryRepo(pool))
	monthly, err := rep.GetMonthlySummary(ctx, tenant, 2019, 10, "en", "EUR", 0, false)
	must(err)
	summary, err := rep.GetSummaryReport(ctx, tenant, "2019-10-01", "2019-10-31", "en", "EUR", 0, false)
	must(err)
	if monthly.TotalExpense.MinorUnits != 3600 || summary.TotalExpense.MinorUnits != 3600 {
		t.Fatalf("monthly %+v summary %+v", monthly, summary)
	}
	// The next Monday's rate must not affect a Sunday operation.
	for _, tx := range list {
		if tx.Amount.CurrencyCode == "EUR" && tx.BaseAmount.MinorUnits != 250000 {
			t.Fatal(tx)
		}
	}
	// Switching display currency is read-only, including stored FX snapshots.
	var count int
	must(pool.DB.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE tenant_id=$1 AND comment='Spain'`, tenant).Scan(&count))
	if count != 3 {
		t.Fatal(count)
	}
}
