package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentBenchmarkRegistry(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Benchmarks','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('benchmarks@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := repo.ConfigureSource(ctx, scope, "Test", true); e != nil {
		t.Fatal(e)
	}
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "INDEX", Name: "Synthetic index", Kind: "index", Currency: "RUB"}
	tx, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, scope, instrument); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	calendar := &md.SessionCalendar{Scope: scope, InstrumentID: instrument.ID, SourceKind: "exchange", ID: "calendar", Revision: 1, From: start, Through: start.AddDate(0, 0, 4), Sessions: []md.TradingSession{{ObservationAt: start, CloseAt: start.Add(18 * time.Hour), AvailableAt: start.Add(19 * time.Hour)}, {ObservationAt: start.AddDate(0, 0, 2), CloseAt: start.Add(66 * time.Hour), AvailableAt: start.Add(67 * time.Hour)}}}
	profile := nav.ConventionProfile{Key: "benchmark-prices", Title: "Reviewed benchmark prices", Evidence: "Synthetic evidence", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{"index": {{Scope: scope, InstrumentID: instrument.ID, From: start, Policy: nav.SeriesPolicy{Kind: "daily_candle", SourceKind: "exchange", Adjustment: "unadjusted", Calendar: calendar}}}}}
	if _, e = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0); e != nil {
		t.Fatal(e)
	}
	p := nav.BenchmarkProfile{Key: "index", Title: "Synthetic price index", Kind: "price_index", Currency: "RUB", Evidence: "Reviewed synthetic benchmark", PriceProfileKey: profile.Key, InstrumentKey: "index", Enabled: true}
	first, e := repo.RegisterBenchmark(ctx, tenant, actor, p, 0)
	if e != nil || first.Version != 1 {
		t.Fatal(first, e)
	}
	retry, e := repo.RegisterBenchmark(ctx, tenant, actor, p, 0)
	if e != nil || retry.Version != 1 || !retry.RecordedAt.Equal(first.RecordedAt) {
		t.Fatal(retry, e)
	}
	list, e := repo.ListBenchmarks(ctx, tenant, actor, "")
	if e != nil || len(list) != 1 || list[0].Kind != "price_index" {
		t.Fatal(list, e)
	}
	list, e = repo.ListBenchmarks(ctx, tenant, uuid.NewString(), "")
	if e != nil || len(list) != 0 {
		t.Fatal("foreign listing", list, e)
	}
	if _, e = repo.RegisterBenchmark(ctx, tenant, uuid.NewString(), p, 0); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal(e)
	}
	changed := p
	changed.Title = "Changed"
	if _, e = repo.RegisterBenchmark(ctx, tenant, actor, changed, 0); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal(e)
	}
	wrong := p
	wrong.Key = "wrong"
	wrong.Kind = "etf_price_proxy"
	if _, e = repo.RegisterBenchmark(ctx, tenant, actor, wrong, 0); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("index relabeled ETF", e)
	}
	wrong.Kind = "price_index"
	wrong.Currency = "USD"
	if _, e = repo.RegisterBenchmark(ctx, tenant, actor, wrong, 0); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("currency relabeled", e)
	}
	// An audit failure must roll back the current pointer as well.
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION reject_benchmark_revision() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test rejection'; END $$; CREATE TRIGGER reject_benchmark_revision BEFORE INSERT ON investment_benchmark_revisions FOR EACH ROW EXECUTE FUNCTION reject_benchmark_revision()`); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.RegisterBenchmark(ctx, tenant, actor, changed, 1); e == nil {
		t.Fatal("ignored audit failure")
	}
	list, e = repo.ListBenchmarks(ctx, tenant, actor, "")
	if e != nil || list[0].Version != 1 {
		t.Fatal(list, e)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER reject_benchmark_revision ON investment_benchmark_revisions`); e != nil {
		t.Fatal(e)
	}

	// Full comparison uses the authorized journal and historical benchmark quotes.
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", start, "benchmark-ledger")
	if e != nil {
		t.Fatal(e)
	}
	for i, amount := range []string{"1000", "500"} {
		_, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "deposit-" + amount, ExpectedLedgerVersion: int64(i + 1), Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: amount, OccurredAt: start.Add(time.Duration(i*24+1) * time.Hour)}})
		if e != nil {
			t.Fatal(e)
		}
	}
	observations := []md.Observation{}
	for i, price := range []string{"100", "120"} {
		observations = append(observations, md.Observation{InstrumentID: instrument.ID, Kind: "daily_candle", At: start.AddDate(0, 0, i*2), SourceKind: "exchange", PriceUnit: "provider_unspecified", Adjustment: "unadjusted", Currency: "RUB", Price: price, Open: price, High: price, Low: price, Volume: "0", Complete: true})
	}
	if _, e = repo.SaveObservations(ctx, scope, observations); e != nil {
		t.Fatal(e)
	}
	request := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market"}
	if _, e = repo.EnqueueDailyNAV(ctx, request, "2024-01-02", "2024-01-03"); e != nil {
		t.Fatal(e)
	}
	for _, date := range []string{"2024-01-02", "2024-01-03"} {
		if _, e = repo.BuildDailyNAV(ctx, request, date); e != nil {
			t.Fatal(e)
		}
	}
	series, _, _ := navSeries(request)
	comparison, e := repo.BenchmarkForSeries(ctx, tenant, actor, series, p.Key, "2024-01-02", "2024-01-03")
	if e != nil || comparison.Result.Reason != "" || len(comparison.Result.Points) != 3 {
		t.Fatal(comparison, e)
	}
	if comparison.Result.Points[1].Cash.Cmp(big.NewRat(500, 1)) != 0 || comparison.Result.Points[2].Value.Cmp(big.NewRat(1700, 1)) != 0 || comparison.PriceProfileVersion != 1 || comparison.Evidence[2].ObservationRevision != 1 {
		t.Fatal(comparison)
	}
	// A later correction must not leak into an existing repeatable-read snapshot.
	snapshot, e := pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = snapshot.Rollback(ctx) }()
	old, e := readBenchmarkComparison(ctx, snapshot, tenant, actor, series, p.Key, "2024-01-02", "2024-01-03")
	if e != nil {
		t.Fatal(e)
	}
	observations[1].Price = "130"
	observations[1].Open = "130"
	observations[1].High = "130"
	observations[1].Low = "130"
	if _, e = repo.SaveObservations(ctx, scope, observations[1:]); e != nil {
		t.Fatal(e)
	}
	stable, e := readBenchmarkComparison(ctx, snapshot, tenant, actor, series, p.Key, "2024-01-02", "2024-01-03")
	if e != nil || stable.Portfolio.Generation != old.Portfolio.Generation || stable.Result.Points[2].Value.Cmp(big.NewRat(1700, 1)) != 0 {
		t.Fatal("mixed revisions", stable, e)
	}
	fresh, e := repo.BenchmarkForSeries(ctx, tenant, actor, series, p.Key, "2024-01-02", "2024-01-03")
	if e != nil || fresh.Portfolio.Generation <= old.Portfolio.Generation || fresh.Result.Points[2].Value.Cmp(big.NewRat(1800, 1)) != 0 || fresh.Evidence[2].ObservationRevision != 2 {
		t.Fatal(fresh, e)
	}
	// An arbitrary period can seed units from an eligible carried close.
	carried, e := repo.BenchmarkForSeries(ctx, tenant, actor, series, p.Key, "2024-01-03", "2024-01-03")
	if e != nil || carried.Result.Reason != "" || carried.Result.Points[1].Value.Cmp(big.NewRat(1950, 1)) != 0 {
		t.Fatal("carried baseline", carried, e)
	}
	if _, e = repo.BenchmarkForSeries(ctx, tenant, uuid.NewString(), series, p.Key, "2024-01-02", "2024-01-03"); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign comparison", e)
	}
	if e = repo.ConfigureSource(ctx, scope, "Test", false); e != nil {
		t.Fatal(e)
	}
	disabled, e := repo.BenchmarkForSeries(ctx, tenant, actor, series, p.Key, "2024-01-02", "2024-01-03")
	if e != nil || disabled.Result.Reason != "initial_benchmark_price_unavailable" {
		t.Fatal("disabled source used", disabled, e)
	}
	p.Enabled = false
	revoked, e := repo.RegisterBenchmark(ctx, tenant, actor, p, 1)
	if e != nil || revoked.Version != 2 {
		t.Fatal(revoked, e)
	}
	list, e = repo.ListBenchmarks(ctx, tenant, actor, "")
	if e != nil || len(list) != 0 {
		t.Fatal(list, e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0036_investment_benchmarks")
}
