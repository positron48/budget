package postgres

import (
	"context"
	"os"
	"os/user"
	"testing"
	"time"

	"github.com/positron48/budget/internal/adapter/tinvest"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// Synthetic financial documents trigger real read-only public market hydration.
// This never reads broker accounts, private files, or a production database.
func TestMarketHistoryHydrationLive(t *testing.T) {
	if os.Getenv("TINVEST_LIVE_TEST") != "1" {
		t.Skip("live public history verification is opt-in")
	}
	pool, done := withPg(t)
	defer done()
	base, tenant, actor := acceptanceOwner(t, pool)
	ctx, cancel := context.WithTimeout(base, 2*time.Minute)
	defer cancel()
	u, err := user.Current()
	if err != nil {
		t.Fatal("macOS account unavailable")
	}
	client, err := tinvest.NewFromKeychain(ctx, u.Username)
	if err != nil {
		t.Fatal("credential unavailable")
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "disposable-hydration"}
	if err = repo.ConfigureSource(ctx, scope, "Synthetic workflow public data", true); err != nil {
		t.Fatal(err)
	}
	client.SetRequestGate(func(ctx context.Context, key string) error {
		return repo.WaitMarketPermit(ctx, key, 600*time.Millisecond)
	})
	client.SetRequestBackoff(repo.DeferMarketPermit)
	id := "e6123145-9665-43e0-8413-cd61b8aa9b13"
	instrument, err := client.LoadMarketInstrument(ctx, id)
	if err != nil {
		t.Fatal("public metadata request failed")
	}
	if instrument.Ticker != "SBER" || instrument.ClassCode != "TQBR" || instrument.Currency != "RUB" {
		t.Fatal("public listing identity changed")
	}
	tx, err := pool.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = saveMarketInstrument(ctx, tx, scope, instrument); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	start := time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Synthetic workflow", start, "ledger")
	if err != nil {
		t.Fatal(err)
	}
	for i, op := range []d.InvestmentOperation{{Kind: "deposit", Currency: "RUB", Amount: "100", OccurredAt: start}, {Kind: "buy", Currency: "RUB", InstrumentKey: "S", Quantity: "1", Amount: "100", OccurredAt: start.Add(time.Hour)}} {
		op.LedgerID = ledger.ID
		if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: op.Kind, ExpectedLedgerVersion: int64(i + 1), Operation: op}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "map", Reason: "Synthetic financial fixture for public hydration", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: id, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: start, Evidence: "Synthetic convention; live listing metadata checked. No valuation acceptance implied."}}}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	recent, err := repo.ScheduleMarket(ctx, scope, at, 50)
	if err != nil || recent != 2 {
		t.Fatal("current demand failed", recent, err)
	}
	backfill, err := repo.ScheduleMarketHistory(ctx, scope, at, 50)
	if err != nil || backfill < 7 {
		t.Fatal("historical demand failed", backfill, err)
	}
	worker := md.Worker{Queue: repo, Provider: client, Scope: scope, Lease: time.Minute}
	for range recent + backfill {
		if err = worker.RunOne(ctx); err != nil {
			t.Fatal("live hydration worker failed", err)
		}
	}
	var ready, pending, failed, observations int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status='ready'),count(*) FILTER(WHERE status IN ('pending','running')),count(*) FILTER(WHERE status='error') FROM market_data_jobs WHERE tenant_id=$1`, tenant).Scan(&ready, &pending, &failed); err != nil || pending != 0 || failed != 0 || ready != recent+backfill {
		t.Fatal("live hydration did not settle", ready, pending, failed, err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_provider_observations WHERE tenant_id=$1 AND kind='daily_candle'`, tenant).Scan(&observations); err != nil || observations < 1800 {
		t.Fatal("historical observations missing", observations, err)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 50); e != nil || n != 0 {
		t.Fatal("hydration repeated the requested history", n, e)
	}
	t.Logf("Live public hydration from synthetic posted buy: %d recent jobs, %d backfill jobs, %d unique candles; no manual enqueue, no session-completeness or valuation claim.", recent, backfill, observations)
}
