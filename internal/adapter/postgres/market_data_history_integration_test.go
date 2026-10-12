package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestMarketHistoryBackfillResumeAndAtomicLimit(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant string
	if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('History backfill','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	id := "11111111-1111-4111-8111-111111111111"
	from := time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := repo.EnqueueHistory(ctx, scope, id, from, to); !errors.Is(err, md.ErrNotFound) {
		t.Fatal(err)
	}
	if err := repo.ConfigureSource(ctx, scope, "T-Invest", true); err != nil {
		t.Fatal(err)
	}
	// Two restarts racing each other must create exactly one set of windows.
	var wg sync.WaitGroup
	results := make(chan []md.Job, 2)
	errorsCh := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			jobs, err := repo.EnqueueHistory(ctx, scope, id, from, to)
			results <- jobs
			errorsCh <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first []md.Job
	for jobs := range results {
		if len(jobs) != 8 {
			t.Fatal(len(jobs))
		}
		if first == nil {
			first = jobs
		} else {
			for i := range jobs {
				if jobs[i].ID != first[i].ID {
					t.Fatal("duplicate backfill")
				}
			}
		}
	}
	// Publish one observed window and one empty window; exhaust another.
	for i := 0; i < 3; i++ {
		j, err := repo.Claim(ctx, scope, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			err = repo.Fail(ctx, j, "provider_rejected", false, 0)
		} else {
			var observations []md.Observation
			if i == 0 {
				observations = []md.Observation{{InstrumentID: id, Kind: "daily_candle", At: j.From, SourceKind: "exchange", PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Price: "9007199254740993.000000001", Open: "9007199254740993.000000001", High: "9007199254740993.000000001", Low: "9007199254740993.000000001", Volume: "1", Complete: true}}
			}
			err = repo.Complete(ctx, j, observations)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	resumed, err := repo.EnqueueHistory(ctx, scope, id, from, to)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, j := range resumed {
		counts[j.Status]++
	}
	if counts["ready"] != 2 || counts["error"] != 1 || counts["pending"] != 5 {
		t.Fatal(counts)
	}
	var empty int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_coverage WHERE tenant_id=$1 AND state='empty_unexplained'`, tenant).Scan(&empty); err != nil || empty != 1 {
		t.Fatal(empty, err)
	}
	// Make the queue almost full; rejecting the last window must roll back
	// every earlier insertion in that same multi-window request.
	if _, err = pool.DB.Exec(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at) SELECT $1,'tinvest','personal',gen_random_uuid(),'last_price','0001-01-01'::timestamptz,'0001-01-01'::timestamptz FROM generate_series(1,94)`, tenant); err != nil {
		t.Fatal(err)
	}
	otherID := "22222222-2222-4222-8222-222222222222"
	if _, err = repo.EnqueueHistory(ctx, scope, otherID, from, to); !errors.Is(err, md.ErrLimit) {
		t.Fatal(err)
	}
	var count int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_jobs WHERE tenant_id=$1 AND instrument_id=$2`, tenant, otherID).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial backfill", count, err)
	}
	// The completed set is still readable while the active queue is full.
	if _, err = repo.EnqueueHistory(ctx, scope, id, from, to); err != nil {
		t.Fatal(err)
	}
	foreign := scope
	foreign.AccessScope = "unprovisioned"
	if _, err = repo.EnqueueHistory(ctx, foreign, id, from, to); !errors.Is(err, md.ErrNotFound) {
		t.Fatal(err)
	}
}
