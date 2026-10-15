package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/positron48/budget/internal/adapter/moex"
	"github.com/positron48/budget/internal/adapter/nativehistory"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestInvestmentTrackerBenchmarksUseNativeUnitsDailyFXAndNoBookWrites(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	l, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Synthetic", PortfolioTracking: true, StartedAt: time.Now()}, "benchmark-tracker")
	if e != nil {
		t.Fatal(e)
	}
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	policy := md.FXPolicy{Key: "benchmark-market", Mode: "market", Source: "synthetic", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "One unit", ValidityConvention: "Explicit"}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	fx := []md.FXPoint{}
	for i, rate := range []string{"90", "100", "120"} {
		at := day.AddDate(0, 0, i+1)
		fx = append(fx, md.FXPoint{From: "USD", To: "RUB", Rate: rate, At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: "synthetic/" + at.Format(time.DateOnly)})
	}
	if _, e = repo.SaveFXPoints(ctx, tenant, policy.Key, 1, fx); e != nil {
		t.Fatal(e)
	}
	for n, b := range nativehistory.Benchmarks() {
		scope := md.Scope{TenantID: tenant, Provider: b.Provider, AccessScope: b.AccessScope}
		if e = repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
			t.Fatal(e)
		}
		observations := []md.Observation{}
		for i := 0; i < 3; i++ {
			price := big.NewRat(int64(100+i*10), 1).RatString()
			if n == 1 {
				price = big.NewRat(int64(10+i), 1).RatString()
			}
			o := md.Observation{InstrumentID: b.Instrument.ID, Kind: "daily_candle", At: day.AddDate(0, 0, i), SourceKind: "exchange", Currency: b.Instrument.Currency, PriceUnit: b.Unit, Adjustment: "unadjusted", Price: price, Open: price, High: price, Low: price, Complete: true}
			if n == 1 {
				o.Volume = "1"
			}
			observations = append(observations, o)
		}
		if _, e = repo.SaveObservations(ctx, scope, observations); e != nil {
			t.Fatal(e)
		}
	}
	q := nav.TrackerHistoryRequest{LedgerIDs: []string{l.ID}, Currency: "RUB", FXPolicyKey: policy.Key, From: "2024-01-03", To: "2024-01-04"}
	lines, e := repo.TrackerBenchmarks(ctx, tenant, actor, q, "2024-01-02")
	if e != nil || len(lines) != 2 {
		t.Fatal(lines, e)
	}
	for n, wants := range [][]string{{"100", "110", "120"}, {"100", "122.222222222222", "160"}} {
		for i, want := range wants {
			v, _ := new(big.Rat).SetString(want)
			if lines[n].Points[i].Index == nil || lines[n].Points[i].Index.Cmp(v) != 0 {
				t.Fatal("historical FX/point units", n, i, lines[n].Points[i])
			}
		}
	}
	if lines[1].Kind != "etf_price_proxy" || lines[1].NativeCurrency != "USD" || lines[0].Kind != "price_index" {
		t.Fatal(lines)
	}
	if _, e = repo.TrackerBenchmarks(ctx, tenant, uuid.NewString(), q, "2024-01-02"); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal(e)
	}
	if e = repo.ConfigureSource(ctx, md.Scope{TenantID: tenant, Provider: "moex", AccessScope: "personal"}, "Synthetic revoked", false); e != nil {
		t.Fatal(e)
	}
	lines, e = repo.TrackerBenchmarks(ctx, tenant, actor, q, "2024-01-02")
	if e != nil || lines[0].Points[0].Index != nil || lines[1].Points[2].Index == nil {
		t.Fatal("one source must not erase the other", lines, e)
	}
	var count int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("benchmark wrote personal journal", count, e)
	}
}

type builtinIndexFeed struct{ nativeFixtureFeed }

func (*builtinIndexFeed) IndexHistory(_ context.Context, from, to string) (moex.History, error) {
	at, _ := time.Parse(time.DateOnly, from)
	b := nativehistory.Benchmarks()[0]
	return moex.History{Observations: []md.Observation{{InstrumentID: b.Instrument.ID, Kind: "daily_candle", At: at, SourceKind: "exchange", Currency: "RUB", PriceUnit: "index_points", Adjustment: "unadjusted", Price: "100", Open: "100", High: "100", Low: "100", Complete: true}}}, nil
}

func TestInvestmentNativeBuiltinBenchmarkDemandUsesEnabledSources(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, _ := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	b := nativehistory.Benchmarks()[0]
	scope := md.Scope{TenantID: tenant, Provider: b.Provider, AccessScope: b.AccessScope}
	at := time.Now().UTC().Add(time.Minute)
	if e := repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	if i, e := repo.NativeMarketInstrument(ctx, scope, b.Instrument.ID); e != nil || i != b.Instrument {
		t.Fatal(i, e)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at, 50); e != nil || n != 1 {
		t.Fatal("recent benchmark demand", n, e)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 50); e != nil || n < 1 || n > 20 {
		t.Fatal("bounded historical demand", n, e)
	}
	feed := &builtinIndexFeed{}
	provider := &nativehistory.Provider{Scope: scope, MOEX: feed, Lookup: func(ctx context.Context, id string) (md.Instrument, error) {
		return repo.NativeMarketInstrument(ctx, scope, id)
	}, Now: func() time.Time { return at }}
	if e := (md.Worker{Queue: repo, Provider: provider, Scope: scope, Lease: time.Minute}).RunOne(ctx); e != nil {
		t.Fatal("native index worker failed", e)
	}
	var saved int
	if e := pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_provider_observations WHERE tenant_id=$1 AND instrument_id=$2`, tenant, b.Instrument.ID).Scan(&saved); e != nil || saved == 0 {
		t.Fatal("index was not published", saved, e)
	}
	var invalid int
	if e := pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_jobs WHERE tenant_id=$1 AND (kind<>'daily_candle' OR instrument_id<>$2::uuid)`, tenant, b.Instrument.ID).Scan(&invalid); e != nil || invalid != 0 {
		t.Fatal(invalid, e)
	}
	if e := repo.ConfigureSource(ctx, scope, "Synthetic revoked", false); e != nil {
		t.Fatal(e)
	}
	if _, e := repo.NativeMarketInstrument(ctx, scope, b.Instrument.ID); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("disabled source recovered", e)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at.AddDate(0, 0, 1), 50); e != nil || n != 0 {
		t.Fatal(n, e)
	}
}
