package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Uses the independently reconciled holdings/prices/dated-FX fixture. A new
// currency must preserve all scope/method fields and all reporting dates.
func assertInvestmentNAVVariants(t *testing.T, ctx context.Context, r *MarketDataRepo, q nav.Request, source string) {
	t.Helper()
	tenant, actor := q.TenantID, q.UserID
	v, e := r.NAVVariant(ctx, tenant, actor, source, "RUB", "market")
	if e != nil || v.Registered || v.Series.From != "2024-01-01" || v.Series.To != "2024-01-02" || !reflect.DeepEqual(v.Series.LedgerIDs, q.LedgerIDs) {
		t.Fatal(v, e)
	}
	var watches int
	if e = r.pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_nav_watches WHERE tenant_id=$1 AND series_key=$2`, tenant, v.Series.Key).Scan(&watches); e != nil || watches != 0 {
		t.Fatal("read registered a watch", watches, e)
	}
	type result struct {
		v nav.Variant
		e error
	}
	ch := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			out, err := r.PrepareNAVVariant(ctx, tenant, actor, source, "RUB", "market")
			ch <- result{out, err}
		}()
	}
	a, b := <-ch, <-ch
	if a.e != nil || b.e != nil || a.v.Series.Key != v.Series.Key || b.v.Series.Key != v.Series.Key || a.v.QueuedDays+b.v.QueuedDays != 2 {
		t.Fatal(a, b, v)
	}
	target := q
	target.Currency = "RUB"
	key, _, _ := navSeries(target)
	if key != v.Series.Key {
		t.Fatal("price rules or account scope changed")
	}
	if _, e = r.pool.DB.Exec(ctx, `UPDATE investment_nav_watches SET cursor_generation=7 WHERE tenant_id=$1 AND series_key=$2`, tenant, key); e != nil {
		t.Fatal(e)
	}
	again, e := r.PrepareNAVVariant(ctx, tenant, actor, source, "RUB", "market")
	if e != nil || again.QueuedDays != 0 {
		t.Fatal(again, e)
	}
	var generation int64
	if e = r.pool.DB.QueryRow(ctx, `SELECT cursor_generation FROM investment_nav_watches WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3`, tenant, actor, key).Scan(&generation); e != nil || generation != 7 {
		t.Fatal("retry reset watch", generation, e)
	}
	for _, date := range []string{"2024-01-01", "2024-01-02"} {
		if _, e = r.BuildDailyNAV(ctx, target, date); e != nil {
			t.Fatal(e)
		}
	}
	history, e := r.NAVHistoryForSeries(ctx, tenant, actor, key, "2024-01-01", "2024-01-02")
	if e != nil || len(history) != 2 || history[0].Result.KnownValue.RatString() != "1028" || history[1].Result.KnownValue.RatString() != "1038" {
		t.Fatal(history, e)
	}
	p, e := r.PerformanceForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-02")
	if e != nil || p.Profit.RatString() != "10" {
		t.Fatal(p, e)
	}
	// Missing market EUR history remains a gap even though a separate CBR policy
	// exists and can calculate the same dates with its own historical fixings.
	cbr := md.FXPolicy{Key: "variant_cbr", Mode: "cbr", Source: "test_cbr", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}
	if _, e = r.RegisterFXPolicy(ctx, tenant, actor, cbr, 0); e != nil {
		t.Fatal(e)
	}
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, rate := range []string{"5", "10"} {
		day := at.AddDate(0, 0, i)
		if _, e = r.SaveFXPoints(ctx, tenant, cbr.Key, 1, []md.FXPoint{{From: "EUR", To: "RUB", Rate: rate, At: day, AvailableAt: day, ValidUntil: day.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("c", 64), RecordReference: uuid.NewString()}}); e != nil {
			t.Fatal(e)
		}
	}
	for _, fx := range []string{"market", cbr.Key} {
		variant, e := r.PrepareNAVVariant(ctx, tenant, actor, source, "EUR", fx)
		if e != nil {
			t.Fatal(e)
		}
		target.Currency = "EUR"
		target.FXPolicyKey = fx
		for _, date := range []string{"2024-01-01", "2024-01-02"} {
			if _, e = r.BuildDailyNAV(ctx, target, date); e != nil {
				t.Fatal(e)
			}
		}
		history, e = r.NAVHistoryForSeries(ctx, tenant, actor, variant.Series.Key, "2024-01-01", "2024-01-02")
		if e != nil || len(history) != 2 {
			t.Fatal(history, e)
		}
		if fx == "market" {
			if history[0].State != "incomplete" || history[1].State != "incomplete" {
				t.Fatal("CBR fallback", history)
			}
		} else if history[0].Result.KnownValue.RatString() != "1028/5" || history[1].Result.KnownValue.RatString() != "519/5" {
			t.Fatal("used one current rate for entire history", history)
		}
	}
	for _, currency := range []string{"GBP", "", "usd"} {
		if _, e = r.PrepareNAVVariant(ctx, tenant, actor, source, currency, "market"); !errors.Is(e, d.ErrInvestmentInvalid) {
			t.Fatal(currency, e)
		}
	}
	if _, e = r.PrepareNAVVariant(ctx, uuid.NewString(), actor, source, "RUB", "market"); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("foreign tenant", e)
	}
	if _, e = r.NAVVariant(ctx, tenant, uuid.NewString(), source, "RUB", "market"); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign actor", e)
	}
	if _, e = r.PrepareNAVVariant(ctx, tenant, actor, source, "RUB", "missing"); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(e)
	}
	if _, e = r.pool.DB.Exec(ctx, `UPDATE investment_nav_watches SET enabled=false WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3`, tenant, actor, source); e != nil {
		t.Fatal(e)
	}
	if _, e = r.PrepareNAVVariant(ctx, tenant, actor, source, "RUB", "market"); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("inactive source reactivated", e)
	}
	if _, e = r.pool.DB.Exec(ctx, `UPDATE investment_nav_watches SET enabled=true WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3`, tenant, actor, source); e != nil {
		t.Fatal(e)
	}
	for _, date := range []string{"2024-01-01", "2024-01-02"} {
		if _, e = r.BuildDailyNAV(ctx, q, date); e != nil {
			t.Fatal(e)
		}
	}

}
