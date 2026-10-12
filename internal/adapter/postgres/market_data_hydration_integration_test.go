package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func historyHydrationFixture(t *testing.T, pool *Pool) (context.Context, string, string, *inv.Service, *MarketDataRepo, md.Scope, d.InvestmentLedger, md.MappingChange) {
	t.Helper()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "History", start, "ledger")
	if err != nil {
		t.Fatal(err)
	}
	for i, op := range []d.InvestmentOperation{{Kind: "deposit", Amount: "100", OccurredAt: start}, {Kind: "buy", InstrumentKey: "S", Quantity: "1", Amount: "50", OccurredAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}} {
		op.LedgerID = ledger.ID
		op.Currency = "RUB"
		if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(i + 1), Operation: op}); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if err = repo.ConfigureSource(ctx, scope, "Synthetic", true); err != nil {
		t.Fatal(err)
	}
	change := md.MappingChange{InstrumentKey: "S", RequestKey: "map", Reason: "Reviewed historical listings"}
	for i, p := range []md.MappingPeriod{{From: start, To: time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)}, {From: time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)}, {From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}} {
		p.Provider = scope.Provider
		p.AccessScope = scope.AccessScope
		p.InstrumentID = uuid.NewString()
		p.Kind = "share"
		p.Currency = "RUB"
		p.Unit = "currency_per_unit"
		p.Evidence = "Synthetic reviewed venue"
		tx, err := pool.DB.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = saveMarketInstrument(ctx, tx, scope, md.Instrument{ID: p.InstrumentID, Ticker: "S", Name: "Synthetic", Kind: "share", ClassCode: []string{"OLD", "MID", "NEW"}[i], Currency: "RUB"}); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		change.Periods = append(change.Periods, p)
	}
	if _, err = repo.SaveMapping(ctx, tenant, actor, change); err != nil {
		t.Fatal(err)
	}
	return ctx, tenant, actor, svc, repo, scope, ledger, change
}

func TestMarketHistoryHydrationMappingPeriodsAndContinuation(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor, svc, repo, scope, ledger, change := historyHydrationFixture(t, pool)
	at := time.Now().UTC().Truncate(24 * time.Hour)
	// Competing replicas share the same request checkpoint and global cap.
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, e := repo.ScheduleMarketHistory(ctx, scope, at, 2)
			if e == nil && n != 2 {
				t.Errorf("first bounded batch %d", n)
			}
			failures <- e
		}()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 10; i++ {
		n, e := repo.ScheduleMarketHistory(ctx, scope, at, 2)
		if e != nil || n > 2 {
			t.Fatal(n, e)
		}
		if n == 0 {
			break
		}
		if i == 9 {
			t.Fatal("hydration did not settle")
		}
	}
	rows, err := pool.DB.Query(ctx, `SELECT instrument_id::text,from_at,to_at FROM market_data_jobs WHERE tenant_id=$1 ORDER BY from_at`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for rows.Next() {
		var id string
		var from, to time.Time
		if err = rows.Scan(&id, &from, &to); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		valid := false
		for _, p := range change.Periods {
			if p.InstrumentID == id && !from.Before(p.From) && (p.To.IsZero() || to.Before(p.To)) {
				valid = true
			}
		}
		if !valid || to.Sub(from) > 365*24*time.Hour {
			rows.Close()
			t.Fatal("crossed listing boundary", id, from, to)
		}
		seen[id]++
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 {
		t.Fatal("historical listing was omitted", seen)
	}
	// One exhausted error and one unexplained empty result are not retried by
	// hydration, and their states are preserved instead of declaring coverage.
	for i := 0; i < 2; i++ {
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
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 100); e != nil || n != 0 {
		t.Fatal("terminal request silently repeated", n, e)
	}
	var empty, failed int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_coverage WHERE tenant_id=$1 AND state='empty_unexplained'`, tenant).Scan(&empty); err != nil || empty != 1 {
		t.Fatal(empty, err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_jobs WHERE tenant_id=$1 AND status='error'`, tenant).Scan(&failed); err != nil || failed != 1 {
		t.Fatal(failed, err)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at.AddDate(0, 0, 1), 100); e != nil || n != 1 {
		t.Fatal("new day reloaded the full last year", n, e)
	}
	var from, to time.Time
	if err = pool.DB.QueryRow(ctx, `SELECT from_at,to_at FROM market_data_jobs WHERE tenant_id=$1 AND instrument_id=$2 ORDER BY created_at DESC LIMIT 1`, tenant, change.Periods[2].InstrumentID).Scan(&from, &to); err != nil || !from.Equal(at) || !to.Equal(at.AddDate(0, 0, 1)) {
		t.Fatal("daily extension lost boundaries", from, to, err)
	}
	// Inserting a genuine earlier trade expands only the missing prefix.
	earlier := time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "earlier", ExpectedLedgerVersion: 3, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "S", Quantity: "1", Currency: "RUB", Amount: "50", OccurredAt: earlier}}); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at.AddDate(0, 0, 1), 100); e != nil || n != 1 {
		t.Fatal("earlier import prefix missing", n, e)
	}
	if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "close", ExpectedLedgerVersion: 4, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "sell", InstrumentKey: "S", Quantity: "2", Currency: "RUB", Amount: "100", OccurredAt: at}}); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at.AddDate(0, 0, 3), 100); e != nil || n != 0 {
		t.Fatal("closed position kept extending history", n, e)
	}
	// Read-only EXPLAIN of the exact grouped demand query on committed trades.
	rows, err = pool.DB.Query(ctx, "EXPLAIN "+marketHistoryDemandSQL, tenant, at)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		t.Log(line)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketHistoryHydrationCapacityAndScope(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, _, _, repo, scope, _, _ := historyHydrationFixture(t, pool)
	at := time.Now().UTC()
	if _, err := pool.DB.Exec(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at) SELECT $1,'tinvest','personal',gen_random_uuid(),'last_price','0001-01-01'::timestamptz,'0001-01-01'::timestamptz FROM generate_series(1,100)`, tenant); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 50); e != nil || n != 0 {
		t.Fatal("capacity exceeded", n, e)
	}
	if _, err := pool.DB.Exec(ctx, `UPDATE market_data_jobs SET status='error',error_code='provider_rejected' WHERE id=(SELECT id FROM market_data_jobs WHERE tenant_id=$1 AND status='pending' LIMIT 1)`, tenant); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 50); e != nil || n != 1 {
		t.Fatal("did not resume at the free slot", n, e)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 50); e != nil || n != 0 {
		t.Fatal("active cap not shared", n, e)
	}
	// The same local key in another tenant uses its own reviewed listings and cap.
	ctx2, tenant2, _, _, repo2, scope2, _, _ := historyHydrationFixture(t, pool)
	if n, e := repo2.ScheduleMarketHistory(ctx2, scope2, at, 50); e != nil || n < 7 {
		t.Fatal("another tenant inherited the full queue", n, e)
	}
	var leaked int
	if err := pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_jobs a JOIN market_data_jobs b ON a.instrument_id=b.instrument_id WHERE a.tenant_id=$1 AND b.tenant_id=$2`, tenant, tenant2).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatal("tenant-local instrument key crossed listings", leaked, err)
	}
	foreign := scope
	foreign.AccessScope = "another"
	if n, e := repo.ScheduleMarketHistory(ctx, foreign, at, 50); e != nil || n != 0 {
		t.Fatal("unprovisioned source", n, e)
	}
	if err := repo.ConfigureSource(ctx, foreign, "Independent", true); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, foreign, at, 50); e != nil || n != 0 {
		t.Fatal("other entitlement used personal mappings", n, e)
	}
	if err := repo.ConfigureSource(ctx, scope, "Disabled", false); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 50); e != nil || n != 0 {
		t.Fatal("disabled source", n, e)
	}
}

func TestMarketHistoryHydrationInitiallyClosedPosition(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor, svc, repo, scope, ledger, change := historyHydrationFixture(t, pool)
	closed := time.Date(2021, 1, 1, 12, 0, 0, 0, time.UTC)
	if _, err := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "already-closed", ExpectedLedgerVersion: 3, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "sell", InstrumentKey: "S", Quantity: "1", Currency: "RUB", Amount: "50", OccurredAt: closed}}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	if n, e := repo.ScheduleMarket(ctx, scope, at, 50); e != nil || n != 0 {
		t.Fatal("closed historical position subscribed to current prices", n, e)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 50); e != nil || n != 2 {
		t.Fatal("closed position lost its initial history", n, e)
	}
	var newest time.Time
	var listings int
	if err := pool.DB.QueryRow(ctx, `SELECT max(to_at),count(*) FILTER(WHERE instrument_id<>$2::uuid) FROM market_data_jobs WHERE tenant_id=$1`, tenant, change.Periods[0].InstrumentID).Scan(&newest, &listings); err != nil || listings != 0 || !newest.Equal(closed.Truncate(24*time.Hour).AddDate(0, 0, 1)) {
		t.Fatal("closed position used a later listing or current history", newest, listings, err)
	}
}
