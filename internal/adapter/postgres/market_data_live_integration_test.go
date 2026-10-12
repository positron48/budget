package postgres

import (
	"context"
	"os"
	"os/user"
	"testing"
	"time"

	"github.com/positron48/budget/internal/adapter/tinvest"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// Opt-in evidence only: uses a disposable database and public historical prices.
// Never reads or mutates broker accounts; the Keychain token stays in the client.
func TestMarketWorkerLivePublicHistory(t *testing.T) {
	if os.Getenv("TINVEST_LIVE_TEST") != "1" {
		t.Skip("set TINVEST_LIVE_TEST=1 for read-only live market verification")
	}
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var tenant string
	if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Disposable market proof','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal("macOS account unavailable")
	}
	client, err := tinvest.NewFromKeychain(ctx, u.Username)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewMarketDataRepo(pool)
	client.SetRequestGate(func(ctx context.Context, key string) error {
		return repo.WaitMarketPermit(ctx, key, 600*time.Millisecond)
	})
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "disposable-proof"}
	from := time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC)
	request := md.JobRequest{InstrumentID: "e6123145-9665-43e0-8413-cd61b8aa9b13", Kind: "daily_candle", From: from, To: from.AddDate(0, 0, 7)}
	job, err := repo.Enqueue(ctx, scope, request)
	if err != nil {
		t.Fatal(err)
	}
	worker := md.Worker{Queue: repo, Provider: client, Scope: scope, Lease: 2 * time.Minute}
	if err = worker.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	observations, err := repo.Observations(ctx, scope, request.InstrumentID, request.Kind, request.From, request.To)
	if err != nil || len(observations) == 0 {
		t.Fatal("public history was not persisted", err)
	}
	for _, o := range observations {
		if o.Revision != 1 || o.Currency != "RUB" || o.SourceKind != "exchange" || o.PriceUnit != "provider_unspecified" {
			t.Fatal("lost source semantics")
		}
	}
	var state string
	var count int
	if err = pool.DB.QueryRow(ctx, `SELECT j.status,c.observation_count FROM market_data_jobs j JOIN market_data_coverage c ON c.tenant_id=j.tenant_id AND c.job_id=j.id WHERE j.id=$1`, job.ID).Scan(&state, &count); err != nil || state != "ready" || count != len(observations) {
		t.Fatal("inconsistent published job", state, count, err)
	}
	catalog, err := repo.SearchCatalog(ctx, scope, "SBER", 50, 0)
	if err != nil || len(catalog) != 1 || catalog[0].ID != request.InstrumentID || catalog[0].ClassCode != "TQBR" {
		t.Fatal("provider catalog was not published with observations", err)
	}

	search, err := repo.Enqueue(ctx, scope, md.JobRequest{Kind: "catalog_search", SearchQuery: "SBER", InstrumentKind: "share"})
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	var searchState string
	var candidateCount int
	if err = pool.DB.QueryRow(ctx, `SELECT j.status,c.instrument_count FROM market_data_jobs j JOIN market_data_coverage c ON c.tenant_id=j.tenant_id AND c.job_id=j.id WHERE j.id=$1`, search.ID).Scan(&searchState, &candidateCount); err != nil || searchState != "ready" || candidateCount < 1 {
		t.Fatal("external search failed", searchState, candidateCount, err)
	}
	catalog, err = repo.SearchCatalog(ctx, scope, "SBER", 50, 0)
	if err != nil || len(catalog) < 1 {
		t.Fatal(err)
	}
	for _, i := range catalog {
		if i.ID == request.InstrumentID && (!i.Detailed || i.Currency != "RUB") {
			t.Fatal("summary erased live detailed metadata")
		}
	}
	t.Logf("Live provider catalog search published candidates: %d", candidateCount)
	t.Logf("Live public candles persisted atomically: %d; disposable database", count)
}

func TestMarketHistoryBackfillLive(t *testing.T) {
	testMarketHistoryBackfillLive(t, false)
}

func TestMarketRequestedHistoryLive(t *testing.T) {
	testMarketHistoryBackfillLive(t, true)
}

func testMarketHistoryBackfillLive(t *testing.T, public bool) {
	if os.Getenv("TINVEST_LIVE_TEST") != "1" {
		t.Skip("read-only live market verification is opt-in")
	}
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var tenant string
	if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Disposable historical backfill','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal("macOS account unavailable")
	}
	client, err := tinvest.NewFromKeychain(ctx, u.Username)
	if err != nil {
		t.Fatal("credential unavailable")
	}
	repo := NewMarketDataRepo(pool)
	client.SetRequestGate(func(ctx context.Context, key string) error {
		return repo.WaitMarketPermit(ctx, key, 600*time.Millisecond)
	})
	client.SetRequestBackoff(repo.DeferMarketPermit)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "disposable-history"}
	if err = repo.ConfigureSource(ctx, scope, "T-Invest", true); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	id := "e6123145-9665-43e0-8413-cd61b8aa9b13"
	enqueue := repo.EnqueueHistory
	if public {
		var actor string
		if err = pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('history-live@example.test','synthetic') RETURNING id`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
			t.Fatal(err)
		}
		svc := md.NewService(repo, NewTenantRepo(pool))
		enqueue = func(ctx context.Context, scope md.Scope, id string, from, to time.Time) ([]md.Job, error) {
			return svc.RequestHistory(ctx, tenant, actor, scope, md.JobRequest{InstrumentID: id, Kind: "daily_candle", From: from, To: to})
		}
	}
	jobs, err := enqueue(ctx, scope, id, from, to)
	if err != nil || len(jobs) != 8 {
		t.Fatal("history plan failed", err)
	}
	worker := md.Worker{Queue: repo, Provider: client, Scope: scope, Lease: time.Minute}
	for _, planned := range jobs {
		if err = worker.RunOne(ctx); err != nil {
			t.Fatal("history worker failed", err)
		}
		status, statusErr := repo.JobStatus(ctx, tenant, planned.ID)
		if statusErr != nil {
			t.Fatal("history status unavailable")
		}
		if status.Status != "ready" {
			t.Fatalf("live history attempt: state=%s code=%s attempts=%d", status.Status, status.ErrorCode, status.Attempts)
		}
	}
	resumed, err := enqueue(ctx, scope, id, from, to)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for i, j := range resumed {
		if j.ID != jobs[i].ID || j.Status != "ready" {
			t.Fatal("history window did not complete", j.Status)
		}
		status, err := repo.JobStatus(ctx, tenant, j.ID)
		if err != nil || status.CoverageState != "observed" || status.ObservationCount == 0 {
			t.Fatal("missing window observations", err)
		}
		total += status.ObservationCount
	}
	var unique, revisions int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*),COALESCE(max(revision),0) FROM market_provider_observations WHERE tenant_id=$1`, tenant).Scan(&unique, &revisions); err != nil || unique < 1000 || revisions != 1 {
		t.Fatal("history evidence was not persisted idempotently", err)
	}
	t.Logf("Live SBER 2019-10-01 to 2026-10-01: %d ready windows, %d window observations, %d unique observations; all revision 1. Session completeness not inferred.", len(jobs), total, unique)
}
