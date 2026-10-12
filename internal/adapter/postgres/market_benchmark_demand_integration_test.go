package postgres

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestMarketBenchmarkIndependentDemand(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "benchmark-fixture"}
	if err := repo.ConfigureSource(ctx, scope, "Synthetic benchmark", true); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	oldEnd := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	newStart := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	oldID, newID, unrelatedID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	period := func(id string, from, to time.Time) nav.PriceConventionPeriod {
		return nav.PriceConventionPeriod{Scope: scope, InstrumentID: id, From: from, To: to, Policy: nav.SeriesPolicy{Kind: "daily_candle", SourceKind: "exchange", Adjustment: "unadjusted", Calendar: &md.SessionCalendar{Scope: scope, InstrumentID: id, SourceKind: "exchange", ID: "synthetic", Revision: 1, From: start, Through: at}}}
	}
	tx, err := pool.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, id := range []string{oldID, newID, unrelatedID} {
		if _, err = saveMarketInstrument(ctx, tx, scope, md.Instrument{ID: id, Ticker: "SYNTHETIC", Name: "Synthetic index", Kind: "index", Currency: "RUB"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	profile := nav.ConventionProfile{Key: "benchmark-demand", Title: "Synthetic prices", Evidence: "Reviewed fixture", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{
		"index":     {period(oldID, start, oldEnd), period(newID, newStart, time.Time{})},
		"unrelated": {period(unrelatedID, start, time.Time{})},
	}}
	if _, err = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0); err != nil {
		t.Fatal(err)
	}
	benchmark := nav.BenchmarkProfile{Key: "synthetic-index", Title: "Synthetic price index", Kind: "price_index", Currency: "RUB", Evidence: "Reviewed fixture", PriceProfileKey: profile.Key, InstrumentKey: "index", Enabled: true}
	if _, err = repo.RegisterBenchmark(ctx, tenant, actor, benchmark, 0); err != nil {
		t.Fatal(err)
	}
	alias := benchmark
	alias.Key = "synthetic-index-alias"
	if _, err = repo.RegisterBenchmark(ctx, tenant, actor, alias, 0); err != nil {
		t.Fatal(err)
	}
	otherScope := scope
	otherScope.AccessScope = "other-entitlement"
	if err = repo.ConfigureSource(ctx, otherScope, "Separate fixture entitlement", true); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarket(ctx, otherScope, at, 50); e != nil || n != 0 {
		t.Fatal("benchmark crossed access scope", n, e)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, otherScope, at, 50); e != nil || n != 0 {
		t.Fatal("history crossed access scope", n, e)
	}
	// There are no investment ledgers, security legs or manual refreshes.
	var legs int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_security_legs WHERE tenant_id=$1`, tenant).Scan(&legs); err != nil || legs != 0 {
		t.Fatal(legs, err)
	}
	var wg sync.WaitGroup
	results := make(chan int, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); n, e := repo.ScheduleMarket(ctx, scope, at, 50); results <- n; failures <- e }()
	}
	wg.Wait()
	close(results)
	close(failures)
	total := 0
	for n := range results {
		total += n
	}
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	if total != 1 {
		t.Fatal("benchmark needs one EOD job, not a quote or duplicate", total)
	}
	j, err := repo.Claim(ctx, scope, time.Minute)
	if err != nil || j.InstrumentID != newID || j.Kind != "daily_candle" {
		t.Fatal(j, err)
	}
	if err = repo.Complete(ctx, j, nil); err != nil {
		t.Fatal(err)
	}
	// Small passes continue across both historical listings without filling gap.
	for pass := 0; pass < 20; pass++ {
		n, e := repo.ScheduleMarketHistory(ctx, scope, at, 2)
		if e != nil {
			t.Fatal(e)
		}
		if n == 0 {
			break
		}
		if pass == 19 {
			t.Fatal("history did not converge")
		}
	}
	rows, err := pool.DB.Query(ctx, `SELECT instrument_id::text,from_at,to_at FROM market_data_jobs WHERE tenant_id=$1 AND priority=30 ORDER BY instrument_id,from_at`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rows.Next() {
		var id string
		var from, to time.Time
		if err = rows.Scan(&id, &from, &to); err != nil {
			t.Fatal(err)
		}
		if id != oldID && id != newID || id == oldID && (!from.Before(oldEnd) || !to.Before(oldEnd)) || id == newID && from.Before(newStart) || to.Sub(from) > 365*24*time.Hour {
			t.Fatal("unreviewed listing or gap requested", id, from, to)
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || count < 6 {
		t.Fatal(count, err)
	}
	// Empty/exhausted jobs remain a durable request checkpoint, not coverage.
	if _, err = pool.DB.Exec(ctx, `UPDATE market_data_jobs SET status='error',error_code='synthetic_exhausted' WHERE tenant_id=$1 AND priority=30`, tenant); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 50); e != nil || n != 0 {
		t.Fatal("terminal work reissued", n, e)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at.Add(5*time.Hour), 50); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at.Add(6*time.Hour), 50); e != nil || n != 1 {
		t.Fatal("regular EOD missing", n, e)
	}
	j, err = repo.Claim(ctx, scope, time.Minute)
	if err != nil || j.InstrumentID != newID {
		t.Fatal(j, err)
	}
	if err = repo.Complete(ctx, j, nil); err != nil {
		t.Fatal(err)
	}
	// Revoking the price profile stops both paths despite an enabled benchmark.
	profile.Enabled = false
	if _, err = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 1); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at.AddDate(0, 0, 1), 50); e != nil || n != 0 {
		t.Fatal("revoked profile polling", n, e)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at.AddDate(0, 0, 1), 50); e != nil || n != 0 {
		t.Fatal("revoked history", n, e)
	}
	profile.Enabled = true
	if _, err = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 2); err != nil {
		t.Fatal(err)
	}
	benchmark.Enabled = false
	if _, err = repo.RegisterBenchmark(ctx, tenant, actor, benchmark, 1); err != nil {
		t.Fatal(err)
	}
	alias.Enabled = false
	if _, err = repo.RegisterBenchmark(ctx, tenant, actor, alias, 1); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at.AddDate(0, 0, 1), 50); e != nil || n != 0 {
		t.Fatal("disabled benchmark polling", n, e)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at.AddDate(0, 0, 1), 50); e != nil || n != 0 {
		t.Fatal("disabled benchmark history", n, e)
	}
	// An explicit card demand survives benchmark revocation independently.
	if _, err = repo.RequestRefresh(ctx, scope, md.JobRequest{InstrumentID: newID, Kind: "daily_candle", From: at.AddDate(0, 0, -1), To: at}); err != nil {
		t.Fatal(err)
	}
	j, err = repo.Claim(ctx, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Complete(ctx, j, nil); err != nil {
		t.Fatal(err)
	}
	// RequestRefresh uses the database clock for its cooldown. The historical
	// scheduler fixture above must not be mistaken for that real refresh time.
	var cardDue, cardUntil time.Time
	if err = pool.DB.QueryRow(ctx, `SELECT next_due,requested_until FROM market_data_subscriptions WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 AND kind='daily_candle'`, tenant, scope.Provider, scope.AccessScope, newID).Scan(&cardDue, &cardUntil); err != nil || !cardDue.Before(cardUntil) {
		t.Fatal("manual refresh demand interval", cardDue, cardUntil, err)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, cardDue.Add(-time.Nanosecond), 50); e != nil || n != 0 {
		t.Fatal("card demand bypassed cooldown", n, e)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, cardDue, 50); e != nil || n != 1 {
		t.Fatal("card demand removed with benchmark", n, e)
	}
	foreign := scope
	foreign.TenantID = uuid.NewString()
	if n, e := repo.ScheduleMarketHistory(ctx, foreign, at, 50); e != nil || n != 0 {
		t.Fatal("foreign demand", n, e)
	}
	if err = repo.ConfigureSource(ctx, scope, "Synthetic", false); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at.AddDate(0, 0, 2), 50); e != nil || n != 0 {
		t.Fatal("disabled source", n, e)
	}
	// Read-only plan for the exact registry lookup used by both schedulers.
	rows, err = pool.DB.Query(ctx, "EXPLAIN "+benchmarkMarketDemandSQL, tenant)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		t.Log(line)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Claim(ctx, foreign, time.Minute); !errors.Is(err, md.ErrNoJob) {
		t.Fatal("foreign claim", err)
	}
}
