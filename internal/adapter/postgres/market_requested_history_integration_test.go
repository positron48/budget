package postgres

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestMarketRequestedHistoryAtomicResume(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if err := repo.ConfigureSource(ctx, scope, "Synthetic", true); err != nil {
		t.Fatal(err)
	}
	svc := md.NewService(repo, NewTenantRepo(pool))
	from := time.Date(1996, 10, 1, 0, 0, 0, 0, time.UTC)
	q := md.JobRequest{InstrumentID: uuid.NewString(), Kind: "daily_candle", From: from, To: from.AddDate(30, 0, 0)}
	var wg sync.WaitGroup
	results := make(chan []md.Job, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := svc.RequestHistory(ctx, tenant, actor, scope, q)
			results <- j
			failures <- e
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first []md.Job
	for jobs := range results {
		if len(jobs) != 31 {
			t.Fatal(len(jobs))
		}
		if first == nil {
			first = jobs
		} else {
			for i, j := range jobs {
				if j.ID != first[i].ID {
					t.Fatal("duplicate batch")
				}
			}
		}
	}
	for i := 0; i < 3; i++ {
		j, e := repo.Claim(ctx, scope, time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			e = repo.Fail(ctx, j, "provider_rejected", false, 0)
		} else {
			e = repo.Complete(ctx, j, nil)
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	// A new repo instance resumes persisted terminal and pending windows.
	resumed, e := NewMarketDataRepo(pool).RequestHistory(ctx, scope, q)
	if e != nil || len(resumed) != 31 {
		t.Fatal(len(resumed), e)
	}
	for i, j := range resumed {
		if j.ID != first[i].ID {
			t.Fatal("restart changed identity")
		}
	}
	status, e := repo.JobStatus(ctx, tenant, resumed[0].ID)
	if e != nil || status.Status != "error" || status.ErrorCode != "provider_rejected" {
		t.Fatal(status, e)
	}
	status, e = repo.JobStatus(ctx, tenant, resumed[1].ID)
	if e != nil || status.CoverageState != "empty_unexplained" {
		t.Fatal(status, e)
	}
	old := md.Observation{InstrumentID: q.InstrumentID, Kind: "daily_candle", At: from.AddDate(0, 0, 1), SourceKind: "exchange", PriceUnit: "provider_unspecified", Adjustment: "unadjusted", Currency: "RUB", Price: "9007199254740993.000000001", Open: "9007199254740993.000000001", High: "9007199254740993.000000001", Low: "9007199254740993.000000001", Volume: "0", Complete: true}
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{old}); e != nil {
		t.Fatal(e)
	}
	observations, e := svc.History(ctx, tenant, actor, scope, q.InstrumentID, "daily_candle", q.From, q.To)
	if e != nil || len(observations) != 1 || observations[0].Price != old.Price {
		t.Fatal("30-year read lost exact evidence", observations, e)
	}
	if _, e = svc.History(ctx, tenant, actor, scope, q.InstrumentID, "daily_candle", q.From, q.To.Add(time.Second)); !errors.Is(e, md.ErrInvalid) {
		t.Fatal("read horizon bypass", e)
	}
	planRows, e := pool.DB.Query(ctx, `EXPLAIN SELECT id,data,revision,received_at FROM market_provider_observations WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 AND kind=$5 AND observed_at BETWEEN $6 AND $7 ORDER BY observed_at,source_kind,adjustment LIMIT 30001`, tenant, scope.Provider, scope.AccessScope, q.InstrumentID, "daily_candle", q.From, q.To)
	if e != nil {
		t.Fatal(e)
	}
	for planRows.Next() {
		var line string
		if e = planRows.Scan(&line); e != nil {
			t.Fatal(e)
		}
		t.Log(line)
	}
	e = planRows.Err()
	planRows.Close()
	if e != nil {
		t.Fatal(e)
	}
	other := q
	other.InstrumentID = uuid.NewString()
	if _, e = repo.RequestHistory(ctx, scope, other); !errors.Is(e, md.ErrLimit) {
		t.Fatal("public burst gate bypass", e)
	}
	var n int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_jobs WHERE tenant_id=$1 AND instrument_id=$2`, tenant, other.InstrumentID).Scan(&n); e != nil || n != 0 {
		t.Fatal("quota rejection wrote jobs", n, e)
	}
	// Leave exactly one slot, then request two windows. Neither may commit.
	if _, e = pool.DB.Exec(ctx, `UPDATE market_data_jobs SET created_at=clock_timestamp()-interval '2 minutes' WHERE tenant_id=$1`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.DB.Exec(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at) SELECT $1,$2,$3,gen_random_uuid(),'last_price','0001-01-01'::timestamptz,'0001-01-01'::timestamptz FROM generate_series(1,71)`, tenant, scope.Provider, scope.AccessScope); e != nil {
		t.Fatal(e)
	}
	// The filler's age is unrelated to capacity and must not test the burst gate.
	if _, e = pool.DB.Exec(ctx, `UPDATE market_data_jobs SET created_at=clock_timestamp()-interval '2 minutes' WHERE tenant_id=$1`, tenant); e != nil {
		t.Fatal(e)
	}
	other.From = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	other.To = other.From.AddDate(1, 0, 0)
	if _, e = repo.RequestHistory(ctx, scope, other); !errors.Is(e, md.ErrLimit) {
		t.Fatal("capacity bypass", e)
	}
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_jobs WHERE tenant_id=$1 AND instrument_id=$2`, tenant, other.InstrumentID).Scan(&n); e != nil || n != 0 {
		t.Fatal("partial batch committed", n, e)
	}
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_subscriptions WHERE tenant_id=$1 AND instrument_id=$2`, tenant, other.InstrumentID).Scan(&n); e != nil || n != 0 {
		t.Fatal("failed batch subscribed", n, e)
	}
	if _, e = svc.RequestHistory(ctx, tenant, uuid.NewString(), scope, q); !errors.Is(e, md.ErrForbidden) {
		t.Fatal("foreign member", e)
	}
	future := q
	future.From = time.Now().UTC().Truncate(time.Second)
	future.To = future.From.Add(time.Hour)
	if _, e = svc.RequestHistory(ctx, tenant, actor, scope, future); !errors.Is(e, md.ErrInvalid) {
		t.Fatal("future request", e)
	}
	if e = repo.ConfigureSource(ctx, scope, "Synthetic", false); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.RequestHistory(ctx, scope, q); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("disabled source", e)
	}
}
