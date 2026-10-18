package postgres

import (
	"context"
	"github.com/positron48/budget/internal/domain"
	rp "github.com/positron48/budget/internal/usecase/recurringpayment"
	"testing"
	"time"
)

func TestRecurringPaymentsWritePlans_PG(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, user, cat string
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	must(pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Plan fixture','RUB') RETURNING id`).Scan(&tenant))
	must(pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('plans@example.test','test') RETURNING id`).Scan(&user))
	must(pool.DB.QueryRow(ctx, `INSERT INTO categories(tenant_id,kind,code) VALUES($1,'expense','plans') RETURNING id`, tenant).Scan(&cat))
	repo := NewRecurringPaymentRepo(pool)
	result, e := repo.ChangeTemplate(ctx, tenant, user, rp.TemplateChange{Template: domain.PaymentTemplate{Name: "Subscription", CategoryID: cat, CurrencyCode: "RUB", Timezone: "Europe/Moscow", AmountMode: "fixed", FixedAmount: paymentPtr(10000), FirstMonth: "2025-01", Day: 31, IntervalMonths: 1}, RequestKey: "fixture"}, time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC))
	must(e)
	_, e = pool.DB.Exec(ctx, `INSERT INTO recurring_payment_templates(tenant_id,name,category_id,currency_code,note,timezone,amount_mode,fixed_amount,first_month,effective_month,interval_months,day,period_offset,created_by,user_id) SELECT $1,'Fixture '||n,$2,'RUB','','Europe/Moscow','fixed',10000,'2025-01-01','2025-01-01',1,31,0,$3,$3 FROM generate_series(1,19)n`, tenant, cat, user)
	must(e)
	_, e = pool.DB.Exec(ctx, `INSERT INTO recurring_payment_occurrences(tenant_id,template_id,scheduled_month,period_month,due_date,name,category_id,currency_code,note,timezone,amount_mode,exact_amount,user_id) SELECT tenant_id,id,('2025-01-01'::date + n*interval '1 month')::date,('2025-01-01'::date+n*interval '1 month')::date,('2025-01-01'::date+n*interval '1 month')::date,name,category_id,currency_code,note,timezone,amount_mode,fixed_amount,user_id FROM recurring_payment_templates CROSS JOIN generate_series(0,599)n WHERE tenant_id=$1 ON CONFLICT DO NOTHING`, tenant)
	must(e)
	_, e = pool.DB.Exec(ctx, `ANALYZE recurring_payment_occurrences`)
	must(e)
	o := rp.Occurrence(result.Template, time.Date(2076, 1, 1, 0, 0, 0, 0, time.UTC))
	explain := func(sql string, args ...any) {
		t.Helper()
		rows, e := pool.DB.Query(ctx, "EXPLAIN "+sql, args...)
		must(e)
		defer rows.Close()
		for rows.Next() {
			var line string
			must(rows.Scan(&line))
			t.Log(line)
		}
		must(rows.Err())
	}
	explain(insertPaymentOccurrence, o.TenantID, o.TemplateID, o.ScheduledMonth+"-01", o.PeriodMonth+"-01", o.DueDate, o.Name, o.CategoryID, o.CurrencyCode, o.AccountID, o.Note, o.Timezone, o.AmountMode, o.ExactAmount, o.InitialEstimate, o.UserID)
	explain(deleteUntouchedPaymentOccurrences, tenant, result.Template.ID, "2026-10-01")
	var count int
	must(pool.DB.QueryRow(ctx, `SELECT count(*) FROM recurring_payment_occurrences`).Scan(&count))
	if count != 12000 {
		t.Fatal("EXPLAIN mutated fixture", count)
	}
}
