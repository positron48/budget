package postgres

import (
	"context"
	"errors"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"sync"
	"testing"
	"time"
)

func TestMarketJobsLeasesAndPublication(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant string
	if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Jobs','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	request := md.JobRequest{InstrumentID: "11111111-1111-4111-8111-111111111111", Kind: "daily_candle", From: at, To: at.AddDate(0, 0, 7)}
	first, err := repo.Enqueue(ctx, scope, request)
	if err != nil {
		t.Fatal(err)
	}
	again, err := repo.Enqueue(ctx, scope, request)
	if err != nil || again.ID != first.ID {
		t.Fatal("queue dedup", again, err)
	}
	var wg sync.WaitGroup
	jobs := make(chan md.Job, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); j, e := repo.Claim(ctx, scope, time.Minute); jobs <- j; failures <- e }()
	}
	wg.Wait()
	close(jobs)
	close(failures)
	claimed := md.Job{}
	for j := range jobs {
		if j.ID != "" {
			claimed = j
		}
	}
	successes, empty := 0, 0
	for e := range failures {
		if e == nil {
			successes++
		} else if errors.Is(e, md.ErrNoJob) {
			empty++
		} else {
			t.Fatal(e)
		}
	}
	if successes != 1 || empty != 1 || claimed.Attempts != 1 {
		t.Fatal(successes, empty, claimed)
	}
	if err = repo.Renew(ctx, claimed, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	foreign := claimed
	foreign.Scope.AccessScope = "other"
	if err = repo.Complete(ctx, foreign, nil); !errors.Is(err, md.ErrLeaseLost) {
		t.Fatal("scope leak", err)
	}
	// Simulate a crashed worker without a wall-clock sleep.
	if _, err = pool.DB.Exec(ctx, `UPDATE market_data_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, claimed.ID); err != nil {
		t.Fatal(err)
	}
	next, err := repo.Claim(ctx, scope, time.Minute)
	if err != nil || next.ID != claimed.ID || next.LeaseToken == claimed.LeaseToken || next.Attempts != 2 {
		t.Fatal(next, err)
	}
	for _, e := range []error{repo.Complete(ctx, claimed, nil), repo.Renew(ctx, claimed, time.Minute), repo.Fail(ctx, claimed, "provider_temporary", true, time.Second)} {
		if !errors.Is(e, md.ErrLeaseLost) {
			t.Fatal("stale lease accepted", e)
		}
	}
	observation := md.Observation{InstrumentID: request.InstrumentID, Kind: request.Kind, At: at, SourceKind: "exchange", PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Price: "10", Open: "10", Low: "9", High: "11", Volume: "1", Complete: true}
	wrong := observation
	wrong.At = request.To.AddDate(0, 0, 1)
	if err = repo.Complete(ctx, next, []md.Observation{wrong}); !errors.Is(err, md.ErrInvalid) {
		t.Fatal("out-of-range observation", err)
	}
	// Force coverage write failure after observation insert: neither can publish.
	if _, err = pool.DB.Exec(ctx, `CREATE FUNCTION reject_market_coverage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic coverage failure'; END $$; CREATE TRIGGER reject_market_coverage BEFORE INSERT ON market_data_coverage FOR EACH ROW EXECUTE FUNCTION reject_market_coverage()`); err != nil {
		t.Fatal(err)
	}
	if err = repo.Complete(ctx, next, []md.Observation{observation}); err == nil {
		t.Fatal("expected failure")
	}
	found, err := repo.Observations(ctx, scope, request.InstrumentID, request.Kind, at, request.To)
	if err != nil || len(found) != 0 {
		t.Fatal("partial publication", found, err)
	}
	if _, err = pool.DB.Exec(ctx, `DROP TRIGGER reject_market_coverage ON market_data_coverage; DROP FUNCTION reject_market_coverage()`); err != nil {
		t.Fatal(err)
	}
	if err = repo.Complete(ctx, next, []md.Observation{observation}); err != nil {
		t.Fatal(err)
	}
	if err = repo.Complete(ctx, next, []md.Observation{observation}); !errors.Is(err, md.ErrLeaseLost) {
		t.Fatal("completed lease reused", err)
	}
	var state string
	var count int
	if err = pool.DB.QueryRow(ctx, `SELECT state,observation_count FROM market_data_coverage WHERE job_id=$1`, next.ID).Scan(&state, &count); err != nil || state != "observed" || count != 1 {
		t.Fatal(state, count, err)
	}
	fresh, err := repo.Enqueue(ctx, scope, request)
	if err != nil || fresh.ID == first.ID {
		t.Fatal("refresh after completion", fresh, err)
	}
	active, err := repo.Claim(ctx, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Fail(ctx, active, "provider_temporary", true, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Claim(ctx, scope, time.Minute); !errors.Is(err, md.ErrNoJob) {
		t.Fatal("ignored backoff", err)
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE market_data_jobs SET available_at=clock_timestamp(),attempts=4 WHERE id=$1`, active.ID); err != nil {
		t.Fatal(err)
	}
	last, err := repo.Claim(ctx, scope, time.Minute)
	if err != nil || last.Attempts != 5 {
		t.Fatal(last, err)
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE market_data_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, last.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Claim(ctx, scope, time.Minute); !errors.Is(err, md.ErrNoJob) {
		t.Fatal(err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT status FROM market_data_jobs WHERE id=$1`, last.ID).Scan(&state); err != nil || state != "error" {
		t.Fatal("exhausted crashed lease", state, err)
	}
	if _, err = repo.Enqueue(ctx, scope, request); err != nil {
		t.Fatal(err)
	}
	active, err = repo.Claim(ctx, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Complete(ctx, active, nil); err != nil {
		t.Fatal(err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT state FROM market_data_coverage WHERE job_id=$1`, active.ID).Scan(&state); err != nil || state != "empty_unexplained" {
		t.Fatal("empty is not complete coverage", state, err)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0020_market_jobs")
}
