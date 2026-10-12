package postgres

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestMarketSubscriptionsPositionsAndExpiry(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Demand", start, "create")
	if err != nil {
		t.Fatal(err)
	}
	for i, op := range []d.InvestmentOperation{{Kind: "deposit", Amount: "100", OccurredAt: start}, {Kind: "buy", InstrumentKey: "S", Quantity: "1", Amount: "100", OccurredAt: start.Add(time.Hour)}} {
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
	id := uuid.NewString()
	tx, err := pool.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = saveMarketInstrument(ctx, tx, scope, md.Instrument{ID: id, Ticker: "S", Name: "Synthetic", Kind: "share", ClassCode: "TEST", Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "map", Reason: "Reviewed fixture", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: id, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: start, Evidence: "Synthetic listing"}}}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	// An older backfill must not postpone current holdings' quotes and EOD.
	if _, err = repo.EnqueueHistory(ctx, scope, id, start, start.AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); n, e := repo.ScheduleMarket(ctx, scope, at, 50); counts <- n; failures <- e }()
	}
	wg.Wait()
	close(counts)
	close(failures)
	total := 0
	for n := range counts {
		total += n
	}
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	if total != 4 {
		t.Fatal("concurrent scheduler duplicated demand", total)
	}
	finish := func(n int) {
		t.Helper()
		for range n {
			j, e := repo.Claim(ctx, scope, time.Minute)
			if e != nil {
				t.Fatal(e)
			}
			if j.From.Equal(start) {
				t.Fatal("historical backfill starved current demand")
			}
			if j.Kind == "daily_candle" && (!j.From.Equal(at.Truncate(24*time.Hour).AddDate(0, 0, -14)) || !j.To.Equal(at)) {
				t.Fatal("correction range", j)
			}
			if j.Kind == "dividend_facts" {
				if !j.From.Equal(at.Truncate(24*time.Hour)) && !j.To.Equal(at.Truncate(24*time.Hour)) {
					t.Fatal("dividend record windows", j)
				}
				e = repo.CompleteDividendFacts(ctx, j, md.DividendFacts{Instrument: md.Instrument{ID: j.InstrumentID, Ticker: "S", Name: "Synthetic", Kind: "share", Currency: "RUB", Detailed: true}, From: j.From, To: j.To, AsOf: at})
			} else {
				e = repo.Complete(ctx, j, nil)
			}
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	finish(4)
	if n, e := repo.ScheduleMarket(ctx, scope, at.Add(29*time.Minute), 50); e != nil || n != 0 {
		t.Fatal("early polling", n, e)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at.Add(30*time.Minute), 50); e != nil || n != 1 {
		t.Fatal("quote schedule", n, e)
	}
	finish(1)
	// A gap in the confirmed listing schedule stops polling even while held.
	if _, err = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", ExpectedVersion: 1, RequestKey: "end-map", Reason: "Listing interval ends", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: id, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: start, To: at.Add(45 * time.Minute), Evidence: "Synthetic period"}}}); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at.Add(7*time.Hour), 50); e != nil || n != 0 {
		t.Fatal("mapping gap ignored", n, e)
	}
	if _, err = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", ExpectedVersion: 2, RequestKey: "restore-map", Reason: "Reviewed listing restored", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: id, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: start, Evidence: "Synthetic period"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "close", ExpectedLedgerVersion: 3, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "sell", InstrumentKey: "S", Quantity: "1", Currency: "RUB", Amount: "100", OccurredAt: at}}); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at.Add(7*time.Hour), 50); e != nil || n != 0 {
		t.Fatal("closed position still polling", n, e)
	}
	// A card request lives independently of a closed position and expires.
	card := md.JobRequest{InstrumentID: uuid.NewString(), Kind: "last_price"}
	if _, err = repo.RequestRefresh(ctx, scope, card); err != nil {
		t.Fatal(err)
	}
	finish(1)
	if n, e := repo.ScheduleMarket(ctx, scope, at.Add(time.Hour), 50); e != nil || n != 1 {
		t.Fatal("card subscription missing", n, e)
	}
	finish(1)
	if n, e := repo.ScheduleMarket(ctx, scope, at.AddDate(0, 0, 31), 50); e != nil || n != 0 {
		t.Fatal("card demand did not expire", n, e)
	}
	if _, err = repo.RequestRefresh(ctx, scope, md.JobRequest{Kind: "catalog_search", SearchQuery: "s", InstrumentKind: "share"}); err != nil {
		t.Fatal(err)
	}
	var subscribed int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_subscriptions WHERE tenant_id=$1`, tenant).Scan(&subscribed); err != nil || subscribed != 4 {
		t.Fatal("catalog search subscribed candidates", subscribed, err)
	}
	if err = repo.ConfigureSource(ctx, scope, "Synthetic", false); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at.AddDate(0, 0, 1), 50); e != nil || n != 0 {
		t.Fatal("disabled source scheduled", n, e)
	}
	// Inspect the actual demand SQL plan with representative committed legs.
	rows, e := pool.DB.Query(ctx, `EXPLAIN SELECT m.data FROM investment_market_mappings m WHERE m.tenant_id=$1 AND EXISTS (SELECT 1 FROM investment_security_legs s JOIN investment_ledgers l ON l.tenant_id=s.tenant_id AND l.id=s.ledger_id WHERE s.tenant_id=m.tenant_id AND s.instrument_key=m.instrument_key AND s.effective_at<=$2 AND NOT l.archived GROUP BY s.ledger_id HAVING sum(s.quantity)<>0)`, tenant, at)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var plan string
		if e = rows.Scan(&plan); e != nil {
			rows.Close()
			t.Fatal(e)
		}
		t.Log(plan)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
}

func TestMarketSubscriptionsCapacityAndBondWindow(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, _ := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if err := repo.ConfigureSource(ctx, scope, "Synthetic", true); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	id := uuid.NewString()
	if _, err := repo.RequestRefresh(ctx, scope, md.JobRequest{InstrumentID: id, Kind: "bond_facts", From: at.AddDate(0, 0, -1), To: at}); err != nil {
		t.Fatal(err)
	}
	j, err := repo.Claim(ctx, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Fail(ctx, j, "provider_rejected", false, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at) SELECT $1,'tinvest','personal',gen_random_uuid(),'last_price','0001-01-01'::timestamptz,'0001-01-01'::timestamptz FROM generate_series(1,100)`, tenant); err != nil {
		t.Fatal(err)
	}
	later := at.Add(7 * time.Hour)
	if n, e := repo.ScheduleMarket(ctx, scope, later, 50); e != nil || n != 0 {
		t.Fatal("capacity exceeded", n, e)
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE market_data_jobs SET status='error',error_code='provider_rejected' WHERE id=(SELECT id FROM market_data_jobs WHERE tenant_id=$1 AND status='pending' LIMIT 1)`, tenant); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, later, 50); e != nil || n != 1 {
		t.Fatal("capacity resume lost demand", n, e)
	}
	var from, to time.Time
	if err = pool.DB.QueryRow(ctx, `SELECT from_at,to_at FROM market_data_jobs WHERE tenant_id=$1 AND instrument_id=$2 AND status='pending'`, tenant, id).Scan(&from, &to); err != nil {
		t.Fatal(err)
	}
	if !from.Equal(later.Truncate(24*time.Hour)) || to.Sub(from) != 365*24*time.Hour-time.Second {
		t.Fatal("bond provider range", from, to)
	}
	// A still-active request absorbs another scheduler period without N jobs.
	if n, e := repo.ScheduleMarket(ctx, scope, later.Add(7*time.Hour), 50); e != nil || n != 0 {
		t.Fatal("duplicate active bond request", n, e)
	}
}
