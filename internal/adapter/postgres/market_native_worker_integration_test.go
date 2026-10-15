package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/positron48/budget/internal/adapter/moex"
	"github.com/positron48/budget/internal/adapter/nativehistory"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type nativeFixtureFeed struct{ price string }

func (f *nativeFixtureFeed) History(_ context.Context, l moex.Listing, from, to string) (moex.History, error) {
	return f.history(l, from, to, false), nil
}
func (f *nativeFixtureFeed) BondHistory(_ context.Context, l moex.Listing, from, to string) (moex.History, error) {
	return f.history(l, from, to, true), nil
}
func (f *nativeFixtureFeed) history(l moex.Listing, from, to string, bond bool) moex.History {
	first, _ := time.Parse(time.DateOnly, from)
	last, _ := time.Parse(time.DateOnly, to)
	out := moex.History{Listing: l}
	id := l.ID()
	if bond {
		id = l.BondID()
	}
	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		o := md.Observation{InstrumentID: id, Kind: "daily_candle", At: day, SourceKind: "exchange", Adjustment: "unadjusted", Currency: "RUB", PriceUnit: "currency_per_unit", Price: f.price, Open: f.price, High: f.price, Low: f.price, Volume: "1", Complete: true}
		if bond {
			o.PriceUnit = "percent_of_nominal"
			o.Nominal, o.NominalCurrency = "1000", "RUB"
		}
		out.Observations = append(out.Observations, o)
	}
	return out
}

func TestNativeHistoryWorkerRecoversCacheAndCorrectsPricesWithoutBookWrites(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	first := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	at := first.AddDate(0, 2, 0)
	l, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Synthetic", StartedAt: first.Add(-24 * time.Hour), PortfolioTracking: true}, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	scope := md.Scope{TenantID: tenant, Provider: "moex", AccessScope: "personal"}
	batch := HistoricalPriceBatch{Version: 1}
	for n, listing := range []moex.Listing{{SecurityID: "SYNTH", ISIN: "RU0000000001", Board: "TQBR"}, {SecurityID: "RU0000000002", ISIN: "RU0000000002", Board: "TQCB"}} {
		kind, id, key, unit := "share", listing.ID(), "S", "currency_per_unit"
		if n == 1 {
			kind, id, key, unit = "bond", listing.BondID(), "B", "percent_of_nominal"
		}
		i := md.Instrument{Detailed: true, ID: id, Ticker: listing.SecurityID, ISIN: listing.ISIN, ClassCode: listing.Board, Kind: kind, Name: "Synthetic", Currency: "RUB"}
		o := (&nativeFixtureFeed{price: "10"}).history(listing, "2024-01-01", "2024-01-01", n == 1).Observations[0]
		periods := []md.MappingPeriod{{Provider: "moex", AccessScope: "personal", InstrumentID: id, Kind: kind, Currency: "RUB", Unit: unit, From: first.Add(24 * time.Hour), Evidence: "Synthetic reviewed identity", Nominal: o.Nominal, NominalCurrency: o.NominalCurrency}}
		batch.Listings = append(batch.Listings, HistoricalPriceListing{Provider: "moex", AccessScope: "personal", Instrument: i, Observations: []md.Observation{o}, ResponseHashes: []string{strings.Repeat("a", 64)}})
		batch.Mappings = append(batch.Mappings, md.MappingChange{InstrumentKey: key, RequestKey: key, Reason: "Synthetic", HistoricalPeriods: &periods})
		if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(n + 1), Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "buy", InstrumentKey: key, Currency: "RUB", Quantity: "2", Amount: "20", OccurredAt: first.Add(24 * time.Hour)}}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = repo.ImportHistoricalPrices(ctx, tenant, actor, batch, HistoricalPricesSHA256(batch), true); e != nil {
		t.Fatal(e)
	}
	bookHash := func() string {
		var s string
		if e := pool.DB.QueryRow(ctx, `SELECT md5(jsonb_agg(to_jsonb(o) ORDER BY id)::text) FROM investment_operations o WHERE tenant_id=$1`, tenant).Scan(&s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	before := bookHash()
	// Licensed catalog/cache can be absent after a canonical archive restore.
	for _, table := range []string{"market_data_changes", "market_provider_observation_revisions", "market_provider_observations", "market_provider_instrument_revisions", "market_provider_instruments"} {
		if _, e = pool.DB.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenant); e != nil {
			t.Fatal(e)
		}
	}
	for _, listing := range batch.Listings {
		if recovered, e := repo.NativeMarketInstrument(ctx, scope, listing.Instrument.ID); e != nil || recovered != listing.Instrument {
			t.Fatal("canonical query identity unavailable", recovered, e)
		}
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at, 50); e != nil || n != 2 {
		t.Fatal("native recent scheduling", n, e)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at, 50); e != nil || n != 0 {
		t.Fatal("duplicate refresh", n, e)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 50); e != nil || n != 2 {
		t.Fatal("native old history scheduling", n, e)
	}
	var invalid int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_jobs WHERE tenant_id=$1 AND kind<>'daily_candle'`, tenant).Scan(&invalid); e != nil || invalid != 0 {
		t.Fatal("unsupported native jobs", invalid, e)
	}
	feed := &nativeFixtureFeed{price: "10"}
	provider := func() *nativehistory.Provider {
		return &nativehistory.Provider{Scope: scope, MOEX: feed, Lookup: func(ctx context.Context, id string) (md.Instrument, error) {
			return repo.NativeMarketInstrument(ctx, scope, id)
		}, Now: func() time.Time { return at }}
	}
	for n := 0; n < 4; n++ {
		w := md.Worker{Queue: repo, Provider: provider(), Scope: scope, Lease: time.Minute}
		if e = w.RunOne(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if e = (md.Worker{Queue: repo, Provider: provider(), Scope: scope, Lease: time.Minute}).RunOne(ctx); !errors.Is(e, md.ErrNoJob) {
		t.Fatal("queue did not finish", e)
	}
	// A terminal transient failure remains in the audit, but a native cache
	// backfill can recover with a new job after a full-day cooldown.
	if _, e = pool.DB.Exec(ctx, `UPDATE market_data_jobs SET status='error',error_code='provider_temporary',finished_at=$2 WHERE id=(SELECT id FROM market_data_jobs WHERE tenant_id=$1 AND priority=30 ORDER BY id LIMIT 1)`, tenant, at.Add(-25*time.Hour)); e != nil {
		t.Fatal(e)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 50); e != nil || n != 1 {
		t.Fatal("native transient backfill did not recover", n, e)
	}
	if e = (md.Worker{Queue: repo, Provider: provider(), Scope: scope, Lease: time.Minute}).RunOne(ctx); e != nil {
		t.Fatal(e)
	}
	var retained int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_jobs WHERE tenant_id=$1 AND status='error' AND error_code='provider_temporary'`, tenant).Scan(&retained); e != nil || retained != 1 {
		t.Fatal("terminal failure audit was reset", retained, e)
	}
	feed.price = "20"
	for _, listing := range batch.Listings {
		if _, e = repo.Enqueue(ctx, scope, md.JobRequest{InstrumentID: listing.Instrument.ID, Kind: "daily_candle", From: first, To: first.AddDate(0, 0, 2)}); e != nil {
			t.Fatal(e)
		}
		if e = (md.Worker{Queue: repo, Provider: provider(), Scope: scope, Lease: time.Minute}).RunOne(ctx); e != nil {
			t.Fatal(e)
		}
		var price string
		var revision int
		if e = pool.DB.QueryRow(ctx, `SELECT data->>'price',revision FROM market_provider_observations WHERE tenant_id=$1 AND provider='moex' AND instrument_id=$2 AND observed_at=$3`, tenant, listing.Instrument.ID, first).Scan(&price, &revision); e != nil || price != "20.000000000" || revision != 2 {
			t.Fatal("correction not saved exactly", price, revision, e)
		}
	}
	if bookHash() != before {
		t.Fatal("history worker changed posted operations")
	}
	state, e := repo.ReadMarketWorkerHealth(ctx, scope)
	if e != nil || state.QuoteStates != nil {
		t.Fatal("native history advertised live quote coverage", state, e)
	}
	// Free-plan backoff must not fill all tenant slots and prevent live broker
	// quotes. The native provider has twenty active slots across access scopes.
	if _, e = pool.DB.Exec(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at) SELECT $1,'moex','personal',gen_random_uuid(),'daily_candle',$2::timestamptz,$3::timestamptz FROM generate_series(1,20)`, tenant, first, at); e != nil {
		t.Fatal(e)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at.Add(7*time.Hour), 50); e != nil || n != 0 {
		t.Fatal("native queue reservation exceeded", n, e)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at.AddDate(0, 0, 1), 50); e != nil || n != 0 {
		t.Fatal("native history reservation exceeded", n, e)
	}
	brokerScope := scope
	brokerScope.Provider = "tinvest"
	if e = repo.ConfigureSource(ctx, brokerScope, "Synthetic broker", true); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.Enqueue(ctx, brokerScope, md.JobRequest{InstrumentID: uuid.NewString(), Kind: "last_price"}); e != nil {
		t.Fatal("native backlog blocked live broker queue", e)
	}
	if e = repo.ConfigureSource(ctx, scope, "Synthetic", false); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.NativeMarketInstrument(ctx, scope, batch.Listings[0].Instrument.ID); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("disabled native source used", e)
	}
	_, other, _ := acceptanceOwner(t, pool)
	scope.TenantID = other
	if _, e = repo.NativeMarketInstrument(ctx, scope, batch.Listings[0].Instrument.ID); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("cross-tenant native identity leaked", e)
	}
}

func TestNativeDailyQuotaSharedAcrossConcurrentWorkers(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	repo := NewMarketDataRepo(pool)
	ctx := context.Background()
	key := strings.Repeat("a", 64)
	var admitted, limited atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := repo.reserveNativeDaily(ctx, key, 3)
			if e == nil {
				admitted.Add(1)
				return
			}
			var q nativeDailyQuota
			if errors.As(e, &q) && q.Temporary() && q.RetryDelay() > 0 {
				limited.Add(1)
			} else {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 3 || limited.Load() != 17 {
		t.Fatal(admitted.Load(), limited.Load())
	}
	if e := repo.reserveNativeDaily(ctx, strings.Repeat("b", 64), 3); e != nil {
		t.Fatal("independent credential quota", e)
	}
	if e := repo.reserveNativeDaily(ctx, "not-a-key", 3); !errors.Is(e, md.ErrInvalid) {
		t.Fatal(e)
	}
}
