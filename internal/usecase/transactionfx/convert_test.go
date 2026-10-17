package transactionfx

import (
	"context"
	"errors"
	"github.com/positron48/budget/internal/domain"
	"math"
	"testing"
	"time"
)

type quotes struct {
	calls int
	dates []time.Time
}

func (q *quotes) GetRateAsOf(_ context.Context, from, to string, at time.Time) (string, string, error) {
	q.calls++
	q.dates = append(q.dates, at)
	if from == "EUR" && to == "USD" {
		if at.Day() == 2 {
			return "1.2", "cbr", nil
		}
		return "1.1", "cbr", nil
	}
	return "", "", errors.New("missing historical rate")
}
func TestHistoricalConversionAndRequestCache(t *testing.T) {
	q := &quotes{}
	c := New(q)
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 21, 30, 0, 0, time.UTC) // October 2 in Moscow
	tx := domain.Transaction{Amount: domain.Money{CurrencyCode: "EUR", MinorUnits: 2000}, OccurredAt: at}
	for i := 0; i < 2; i++ {
		m, e := c.Convert(ctx, tx, "USD")
		if e != nil || m.MinorUnits != 2400 || m.CurrencyCode != "USD" {
			t.Fatalf("%+v %v", m, e)
		}
	}
	if q.calls != 1 || q.dates[0].Day() != 2 {
		t.Fatalf("not cached/calendar boundary: %+v", q)
	}
	tx.OccurredAt = at.Add(-24 * time.Hour)
	m, e := c.Convert(ctx, tx, "USD")
	if e != nil || m.MinorUnits != 2200 || q.calls != 2 {
		t.Fatalf("%+v %v calls %d", m, e, q.calls)
	}
}
func TestOriginalAndHistoricalSnapshot(t *testing.T) {
	q := &quotes{}
	c := New(q)
	tx := domain.Transaction{Amount: domain.Money{CurrencyCode: "EUR", MinorUnits: 2000}, BaseAmount: domain.Money{CurrencyCode: "RUB", MinorUnits: 222223}, Fx: &domain.FxInfo{RateDecimal: "111.1115"}}
	for _, tc := range []struct {
		currency string
		minor    int64
	}{{"EUR", 2000}, {"RUB", 222223}} {
		m, e := c.Convert(context.Background(), tx, tc.currency)
		if e != nil || m.MinorUnits != tc.minor {
			t.Fatalf("%+v %v", m, e)
		}
	}
	if q.calls != 0 {
		t.Fatal("looked up rate for original/snapshot")
	}
	if _, e := c.Convert(context.Background(), tx, "GBP"); e == nil {
		t.Fatal("missing quote silently accepted")
	}
}
func TestExactRoundingAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		minor int64
		rate  string
		want  int64
	}{{1, "0.5", 1}, {-1, "0.5", -1}, {123, "1.2345", 152}, {math.MaxInt64, "1", math.MaxInt64}} {
		got, e := ConvertMinor(tc.minor, tc.rate)
		if e != nil || got != tc.want {
			t.Fatalf("%+v got %d %v", tc, got, e)
		}
	}
	for _, rate := range []string{"0", "-1", "invalid"} {
		if _, e := ConvertMinor(1, rate); e == nil {
			t.Fatal(rate)
		}
	}
	if _, e := ConvertMinor(math.MaxInt64, "2"); !errors.Is(e, ErrOverflow) {
		t.Fatal(e)
	}
	if _, e := Add(math.MaxInt64, 1); !errors.Is(e, ErrOverflow) {
		t.Fatal(e)
	}
}
