package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/positron48/budget/internal/usecase/fximport"
)

func TestFxImportAndCrossRates_PG(t *testing.T) {
	pool, _ := withPg(t)
	repo := NewFxRepo(pool)
	ctx := context.Background()
	opening := time.Date(2024, 12, 29, 0, 0, 0, 0, time.UTC)
	next := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	quotes := []fximport.Quote{{Currency: "USD", Rate: "100", Date: opening}, {Currency: "EUR", Rate: "125", Date: opening}, {Currency: "USD", Rate: "110", Date: next}, {Currency: "EUR", Rate: "125", Date: next}}
	for i := 0; i < 2; i++ {
		if err := repo.SaveCBRRates(ctx, quotes); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.DB.QueryRow(ctx, "SELECT count(*) FROM fx_rates WHERE provider='cbr'").Scan(&count); err != nil || count != 4 {
		t.Fatalf("duplicate import: %d %v", count, err)
	}
	check := func(from, to, want, provider string, at, date time.Time) {
		t.Helper()
		rows, err := repo.BatchGetRates(ctx, []string{from}, to, at)
		if err != nil || len(rows) != 1 {
			t.Fatalf("%s/%s: %+v %v", from, to, rows, err)
		}
		if rows[0].Rate != want || rows[0].Provider != provider || !rows[0].AsOf.Equal(date) {
			t.Fatalf("%s/%s: %+v", from, to, rows[0])
		}
		single, source, err := repo.GetRateAsOf(ctx, from, to, at)
		if err != nil || single != want || source != provider {
			t.Fatalf("single differs: %s %s %v", single, source, err)
		}
	}
	jan1 := opening.AddDate(0, 0, 3)
	check("USD", "EUR", "0.80000000", "cbr", jan1, opening)
	check("RUB", "USD", "0.01000000", "cbr", jan1, opening)
	check("EUR", "USD", "1.25000000", "cbr", jan1, opening)
	check("USD", "RUB", "100.00000000", "cbr", jan1, opening)
	if _, err := repo.UpsertRate(ctx, "USD", "EUR", "0.9", opening, "manual"); err != nil {
		t.Fatal(err)
	}
	check("USD", "EUR", "0.90000000", "manual", jan1, opening)
	check("USD", "EUR", "0.88000000", "cbr", next, next)
	// CBR dates change at midnight Moscow, not three hours later at midnight UTC.
	check("USD", "EUR", "0.88000000", "cbr", next.Add(-2*time.Hour), next)
	check("USD", "USD", "1", "identity", jan1, jan1)
	if _, err := repo.UpsertRate(ctx, "USD", "EUR", "0.95", next, "manual"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveCBRRates(ctx, quotes); err != nil {
		t.Fatal(err)
	}
	check("USD", "EUR", "0.95000000", "manual", next, next)
	rows, err := repo.BatchGetRates(ctx, []string{"XYZ"}, "USD", next)
	if err != nil || len(rows) != 0 {
		t.Fatalf("invented missing rate: %+v %v", rows, err)
	}
	rows, err = repo.BatchGetRates(ctx, []string{"USD"}, "EUR", opening.AddDate(0, 0, -1))
	if err != nil || len(rows) != 0 {
		t.Fatalf("future leak: %+v %v", rows, err)
	}
	// One overflowing value must prevent publication of every quote in the batch.
	err = repo.SaveCBRRates(ctx, []fximport.Quote{{Currency: "GBP", Rate: "100", Date: opening}, {Currency: "CNY", Rate: "9999999999999", Date: opening}})
	if err == nil {
		t.Fatal("expected overflow")
	}
	if err := pool.DB.QueryRow(ctx, "SELECT count(*) FROM fx_rates WHERE from_currency_code='GBP'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial import: %d %v", count, err)
	}
}
