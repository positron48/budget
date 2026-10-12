package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"math/big"
	"testing"
	"time"
)

func assertInvestmentCurrentNAV(t *testing.T, ctx context.Context, r *MarketDataRepo, q nav.Request, key string) {
	t.Helper()
	read := func(actor, key string, at time.Time) (nav.CurrentNAV, error) {
		tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		v, e := readCurrentNAV(ctx, tx, q.TenantID, actor, key, at)
		if e != nil {
			return v, e
		}
		return v, tx.Commit(ctx)
	}
	var beforeDaily, beforeJobs int
	if e := r.pool.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM investment_daily_nav WHERE tenant_id=$1),(SELECT count(*) FROM investment_nav_jobs WHERE tenant_id=$1)`, q.TenantID).Scan(&beforeDaily, &beforeJobs); e != nil {
		t.Fatal(e)
	}
	priceIs := func(q nav.CurrentQuote, want string) bool {
		if q.Price.Candidate == nil {
			return false
		}
		n, ok := new(big.Rat).SetString(q.Price.Candidate.Price)
		return ok && n.RatString() == want
	}
	at := time.Date(2024, 1, 2, 4, 30, 0, 0, time.UTC)
	v, e := read(q.UserID, key, at)
	if e != nil || !v.Result.Complete || v.Result.KnownValue.RatString() != "519/10" || v.LastCompletedDate != "2024-01-01" || !v.Result.At.Equal(at) || len(v.Quotes) != 1 || v.Quotes[0].Price.State != "fresh" || !priceIs(v.Quotes[0], "130") {
		t.Fatal(v, e)
	}
	misused := v.Result
	misused.At, _ = nav.DayCutoff("2024-01-02")
	if _, e = r.publishDailyNAV(ctx, q, "2024-01-02", misused); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal("provisional published as closed point", e)
	}
	earlier, e := read(q.UserID, key, at.AddDate(0, 0, -1))
	if e != nil || !earlier.Result.Complete || earlier.Result.KnownValue.RatString() != "514/5" || !priceIs(earlier.Quotes[0], "120") {
		t.Fatal("future quote or wrong FX", earlier, e)
	}
	// Before the purchase, no future holding enters the current account valuation.
	before, e := read(q.UserID, key, time.Date(2024, 1, 1, 0, 30, 0, 0, time.UTC))
	if e != nil || !before.Result.Complete || before.Result.KnownValue.RatString() != "100" || len(before.Quotes) != 0 {
		t.Fatal("future operation leaked", before, e)
	}
	rub := q
	rub.Currency = "RUB"
	rubKey, _, _ := navSeries(rub)
	stale, e := read(q.UserID, rubKey, at.AddDate(0, 0, 1).Add(time.Hour))
	if e != nil || stale.Result.Complete || stale.Result.KnownValue.RatString() != "908" || len(stale.Quotes) != 1 || stale.Quotes[0].Price.State != "stale" || stale.Quotes[0].Price.Fact != nil || !priceIs(stale.Quotes[0], "130") {
		t.Fatal("stale price used as full value", stale, e)
	}
	expired, e := read(q.UserID, key, at.AddDate(0, 0, 1).Add(time.Hour))
	if e != nil || expired.Result.Complete || expired.Result.KnownValue.Sign() != 0 {
		t.Fatal("expired FX used", expired, e)
	}
	if _, e = read(uuid.NewString(), key, at); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign actor", e)
	}
	var afterDaily, afterJobs int
	if e = r.pool.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM investment_daily_nav WHERE tenant_id=$1),(SELECT count(*) FROM investment_nav_jobs WHERE tenant_id=$1)`, q.TenantID).Scan(&afterDaily, &afterJobs); e != nil || beforeDaily != afterDaily || beforeJobs != afterJobs {
		t.Fatal("current snapshot persisted", beforeDaily, afterDaily, beforeJobs, afterJobs, e)
	}
}

func assertInvestmentCurrentDailyPolicy(t *testing.T, ctx context.Context, r *MarketDataRepo, q nav.Request, scope md.Scope, instrument md.Instrument, day time.Time) {
	t.Helper()
	c := &md.SessionCalendar{Scope: scope, InstrumentID: instrument.ID, SourceKind: "exchange", ID: "current-test", Revision: 1, From: day, Through: day.Add(72 * time.Hour), Sessions: []md.TradingSession{{ObservationAt: day, CloseAt: day.Add(8 * time.Hour), AvailableAt: day.Add(9 * time.Hour)}}}
	p := nav.ConventionProfile{Key: "current_daily", Title: "Independent current and EOD", Evidence: "Synthetic current quote SLA", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{"S": {{Scope: scope, InstrumentID: instrument.ID, From: day, Policy: nav.SeriesPolicy{Kind: "daily_candle", SourceKind: "exchange", Adjustment: "unadjusted", Calendar: c}}}}}
	if _, e := r.RegisterPriceConventions(ctx, q.TenantID, q.UserID, p, 0); e != nil {
		t.Fatal(e)
	}
	q.ConventionKey = p.Key
	if _, e := r.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-01"); e != nil {
		t.Fatal(e)
	}
	key, _, _ := navSeries(q)
	read := func() nav.CurrentNAV {
		tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		v, e := readCurrentNAV(ctx, tx, q.TenantID, q.UserID, key, day.Add(4*time.Hour+30*time.Minute))
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	without := read()
	if without.Result.Complete || without.Result.KnownValue.RatString() != "900" {
		t.Fatal("implicit last-price policy", without)
	}
	found := false
	for _, i := range without.Result.Issues {
		found = found || i.Code == "current_price_policy_missing"
	}
	if !found {
		t.Fatal("missing policy not explained", without.Result.Issues)
	}
	p.Periods["S"][0].Policy.CurrentMaxQuoteAge = time.Hour
	if _, e := r.RegisterPriceConventions(ctx, q.TenantID, q.UserID, p, 1); e != nil {
		t.Fatal(e)
	}
	current := read()
	if !current.Result.Complete || current.Result.KnownValue.RatString() != "1020" || len(current.Quotes) != 1 || current.Quotes[0].Price.Candidate == nil {
		t.Fatal("explicit last-price policy not used", current)
	}
	if _, e := r.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: instrument.ID, Kind: "daily_candle", At: day, SourceKind: "exchange", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Currency: "RUB", Price: "105", Open: "105", High: "105", Low: "105", Volume: "0", Complete: true}}); e != nil {
		t.Fatal(e)
	}
	daily, e := r.BuildDailyNAV(ctx, q, "2024-01-01")
	if e != nil || daily.State != "ready" || daily.Result.KnownValue.RatString() != "1005" {
		t.Fatal("last price leaked into EOD", daily, e)
	}
	// A later daily close must not replace the independent last-price quote either.
	current = read()
	if current.Result.KnownValue.RatString() != "1020" {
		t.Fatal("daily close leaked into current quote", current)
	}
}
