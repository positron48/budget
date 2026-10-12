package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// Synthetic, explicitly unadjusted prices verify the time rule independently
// from the unresolved split semantics of the personal provider history.
func TestInvestmentRetrospectivePricesPersistRestoreAndCorrect(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	day := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Retrospective", day, "ledger")
	if err != nil {
		t.Fatal(err)
	}
	for i, op := range []d.InvestmentOperation{{Kind: "deposit", Amount: "100", OccurredAt: day}, {Kind: "buy", InstrumentKey: "S", Quantity: "1", Amount: "100", OccurredAt: day.Add(time.Hour)}} {
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
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "S", Name: "Synthetic", Kind: "share", Currency: "RUB"}
	tx, err := pool.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = saveMarketInstrument(ctx, tx, scope, instrument); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "mapping", Reason: "Synthetic", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: day, To: day.Add(24 * time.Hour), Evidence: "Synthetic"}, {Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: day.Add(24 * time.Hour), Evidence: "New terms start at the prior candle effective time"}}}); err != nil {
		t.Fatal(err)
	}
	profile := nav.ConventionProfile{Key: "retrospective", Title: "Retrospective synthetic", Evidence: "Explicit retrospective valuation only", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{"S": {{Scope: scope, InstrumentID: instrument.ID, From: day, Policy: nav.SeriesPolicy{Kind: "daily_candle", SourceKind: "exchange", Adjustment: "unadjusted", Retrospective: &md.RetrospectiveDailyPolicy{Convention: md.RetrospectiveCloseNextUTCDay, MaxAge: 96 * time.Hour}}}}}}
	if _, err = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0); err != nil {
		t.Fatal(err)
	}
	fx := md.FXPolicy{Key: "market", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}
	if _, err = repo.RegisterFXPolicy(ctx, tenant, actor, fx, 0); err != nil {
		t.Fatal(err)
	}
	observations := []md.Observation{}
	for i, price := range []string{"120", "130"} {
		observations = append(observations, md.Observation{InstrumentID: instrument.ID, Kind: "daily_candle", At: day.AddDate(0, 0, i), SourceKind: "exchange", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Currency: "RUB", Price: price, Open: price, High: price, Low: price, Volume: "1", Complete: true})
	}
	if _, err = repo.SaveObservations(ctx, scope, observations); err != nil {
		t.Fatal(err)
	}
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: fx.Key, ConventionKey: profile.Key}
	check := func(r *MarketDataRepo, date, value string, complete bool) {
		t.Helper()
		result, e := r.BuildDailyNAV(ctx, q, date)
		if e != nil || result.Result == nil || result.Result.Complete != complete || result.Result.KnownValue.RatString() != value {
			t.Fatalf("%s: result=%+v error=%v", date, result.Result, e)
		}
		found := false
		for _, w := range result.Result.Warnings {
			found = found || w.Code == "retrospective_price_valuation"
		}
		if !found {
			t.Fatal("retrospective mode hidden", result)
		}
		for _, p := range result.Result.Evidence {
			if p.TimeConvention != md.RetrospectiveCloseNextUTCDay || p.CalendarID != "" || p.CalendarRevision != 0 {
				t.Fatal("fabricated calendar", p)
			}
		}
	}
	if _, err = repo.EnqueueDailyNAV(ctx, q, "2024-01-05", "2024-01-11"); err != nil {
		t.Fatal(err)
	}
	selection := md.PriceSelectionRequest{Scope: scope, InstrumentID: instrument.ID, Kind: "daily_candle", SourceKind: "exchange", Adjustment: "unadjusted", At: day.Add(48 * time.Hour), Retrospective: &md.RetrospectiveDailyPolicy{Convention: md.RetrospectiveCloseNextUTCDay, MaxAge: 96 * time.Hour}}
	oldest := selection.At.Add(-3661 * 24 * time.Hour)
	if _, err = repo.PriceEvidence(ctx, selection, oldest); err != nil {
		t.Fatal("ten-year retrospective history lost preceding UTC identity", err)
	}
	if _, err = repo.PriceEvidence(ctx, selection, oldest.Add(-time.Second)); err == nil {
		t.Fatal("unbounded retrospective history accepted")
	}
	check(repo, "2024-01-05", "0", false)
	check(repo, "2024-01-06", "120", true)
	check(repo, "2024-01-07", "130", true)
	key, _, err := navSeries(q)
	if err != nil {
		t.Fatal(err)
	}
	prices, err := repo.InvestmentInstrumentPrices(ctx, tenant, actor, key, "S", "2024-01-06", "2024-01-06", nil)
	if err != nil || len(prices.Days) != 1 || prices.Days[0].NativeClean == nil || prices.Days[0].NativeClean.RatString() != "120" {
		t.Fatal("instrument history lost D-1 at mapping boundary", prices, err)
	}

	check(repo, "2024-01-11", "0", false)
	observations[0].Price = "121"
	observations[0].Open = "121"
	observations[0].High = "121"
	observations[0].Low = "121"
	if _, err = repo.SaveObservations(ctx, scope, observations[:1]); err != nil {
		t.Fatal(err)
	}
	history, err := repo.DailyNAVHistory(ctx, q, "2024-01-06", "2024-01-06")
	if err != nil || len(history) != 1 || history[0].State != "stale" || history[0].Result != nil {
		t.Fatal("correction did not invalidate dependent close", history, err)
	}
	check(repo, "2024-01-06", "121", true)
	bundle, err := NewInvestmentRepo(pool).ExportInvestmentArchive(ctx, tenant, actor)
	if err != nil {
		t.Fatal(err)
	}
	target, cleanTarget := withPg(t)
	defer cleanTarget()
	if _, err = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restore','RUB')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'retro-restore@example.test','test')`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = NewInvestmentRepo(target).RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); err != nil {
		t.Fatal(err)
	}
	restored := NewMarketDataRepo(target)
	check(restored, "2024-01-06", "0", false)
	if err = restored.ConfigureSource(ctx, scope, "Explicit cache rehydration", true); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.SaveObservations(ctx, scope, observations); err != nil {
		t.Fatal(err)
	}
	check(restored, "2024-01-06", "121", true)
}
