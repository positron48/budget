package recurringpayment

import (
	"github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

func ptr(n int64) *int64 { return &n }
func TestCalendarAnchor(t *testing.T) {
	for _, tc := range []struct {
		month string
		day   int
		last  bool
		want  string
	}{{"2024-02", 31, false, "2024-02-29"}, {"2025-02", 31, false, "2025-02-28"}, {"2025-03", 31, false, "2025-03-31"}, {"2026-04", 31, false, "2026-04-30"}, {"2026-02", 1, true, "2026-02-28"}, {"2026-01", 31, false, "2026-01-31"}} {
		m, _ := Month(tc.month)
		o := Occurrence(domain.PaymentTemplate{Day: tc.day, LastDay: tc.last, PeriodOffset: -1}, m)
		if o.DueDate != tc.want {
			t.Fatalf("%+v: %+v", tc, o)
		}
		if tc.month == "2026-01" && o.PeriodMonth != "2025-12" {
			t.Fatal(o)
		}
	}
}
func TestStatusAndEstimate(t *testing.T) {
	now := time.Date(2026, 3, 29, 22, 30, 0, 0, time.UTC)
	o := domain.PaymentOccurrence{DueDate: "2026-03-29", Timezone: "Europe/Madrid", CurrencyCode: "RUB", InitialEstimate: ptr(100)}
	if Status(o, now) != "overdue" {
		t.Fatal("Madrid DST local date")
	}
	o.Timezone = "UTC"
	if Status(o, now) != "planned" {
		t.Fatal("due day overdue")
	}
	if n, s := Expected(o, ptr(200)); *n != 200 || s != "previous_payment" {
		t.Fatal(n, s)
	}
	o.EstimateOverride = ptr(300)
	if n, _ := Expected(o, ptr(200)); *n != 300 {
		t.Fatal(n)
	}
	o.ExactAmount = ptr(400)
	if n, s := Expected(o, ptr(200)); *n != 400 || s != "exact" {
		t.Fatal(n, s)
	}
	o.Transaction = &domain.Transaction{Type: domain.TransactionTypeExpense, Amount: domain.Money{CurrencyCode: "RUB", MinorUnits: 400}, OccurredAt: now}
	o.ConfirmedAmount = ptr(400)
	o.ConfirmedCurrency = "RUB"
	o.ConfirmedType = "expense"
	if Status(o, now) != "paid" {
		t.Fatal(Status(o, now))
	}
	o.Transaction.Amount.MinorUnits = 401
	if Status(o, now) != "needs_review" {
		t.Fatal("changed amount")
	}
	o.Transaction = nil
	o.Skipped = true
	if Status(o, now) != "skipped" {
		t.Fatal("skipped")
	}
	o.Skipped = false
	o.ExactAmount = nil
	o.InitialEstimate = nil
	o.EstimateOverride = nil
	if n, _ := Expected(o, nil); n != nil {
		t.Fatal("unknown became zero")
	}
}
func TestSummaryIncompleteAndNoDoubleCount(t *testing.T) {
	m := func(n int64) *domain.Money { return &domain.Money{CurrencyCode: "RUB", MinorUnits: n} }
	items := []domain.PaymentOccurrence{{Status: "overdue", ExpectedAmount: ptr(100), EstimateSource: "exact", DisplayAmount: m(100)}, {Status: "planned", ExpectedAmount: ptr(200), EstimateSource: "previous_payment", DisplayAmount: m(200)}, {Status: "planned"}, {Status: "planned", ExpectedAmount: ptr(300), Incomplete: true}, {Status: "needs_review", ExpectedAmount: ptr(500), DisplayAmount: m(500)}, {Status: "paid", DisplayAmount: m(600)}, {Status: "skipped", ExpectedAmount: ptr(700), DisplayAmount: m(700)}}
	s, e := Summary(items, "RUB")
	if e != nil || s.Exact.MinorUnits != 100 || s.Estimated.MinorUnits != 200 || s.Paid.MinorUnits != 600 || s.Unknown != 1 || s.MissingFX != 1 || s.NeedsReview != 1 || s.Outstanding != 4 {
		t.Fatal(s, e)
	}
}
