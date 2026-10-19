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

func TestTrackerEODKeepsHolidayPriceAndScopesUnknownOpening(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	market := NewMarketDataRepo(pool)
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Later metadata", StartedAt: day.AddDate(0, 1, 0), PortfolioTracking: true}, "eod")
	if err != nil {
		t.Fatal(err)
	}
	for i, op := range []d.InvestmentOperation{
		{Kind: "buy", InstrumentKey: "S", Quantity: "2", Amount: "20", Currency: "RUB", Timing: &d.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-01"}},
		{Kind: "opening_position", Note: "Confirmed snapshot with unknown earlier history", InstrumentKey: "UNKNOWN", Quantity: "1", Amount: "0", Currency: "RUB", OccurredAt: day.AddDate(0, 1, 0)},
	} {
		op.LedgerID = ledger.ID
		if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(i + 1), Operation: op}); err != nil {
			t.Fatal(err)
		}
	}
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if err = market.ConfigureSource(ctx, scope, "Synthetic", true); err != nil {
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
	if _, err = market.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "map", Reason: "Synthetic", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: day.AddDate(-1, 0, 0), Evidence: "Synthetic"}}}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ date, price string }{{"2023-12-29", "10"}, {"2024-01-12", "30"}} {
		at, _ := time.Parse(time.DateOnly, v.date)
		_, err = market.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: instrument.ID, Kind: "daily_candle", At: at, SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "provider_unspecified", Price: v.price, Open: v.price, High: v.price, Low: v.price, Volume: "1", Complete: true}})
		if err != nil {
			t.Fatal(err)
		}
	}
	policy := md.FXPolicy{Key: "eod", Mode: "market", Source: "synthetic", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "One unit", ValidityConvention: "Explicit"}
	if _, err = market.RegisterFXPolicy(ctx, tenant, actor, policy, 0); err != nil {
		t.Fatal(err)
	}
	q := nav.TrackerHistoryRequest{LedgerIDs: []string{ledger.ID}, InstrumentKeys: []string{"S"}, Currency: "RUB", FXPolicyKey: policy.Key, From: "2024-01-05", To: "2024-01-10", PageDays: 93}
	h, err := market.TrackerHistory(ctx, tenant, actor, q)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range h.Points {
		if p.State != "ready" || p.KnownValue.RatString() != "20" {
			t.Fatal("holiday price, future price, date-only timing or unrelated opening changed EOD", p)
		}
	}
	// Sparse contribution boundaries must use the same historical price, too.
	q.ContributionOnly = true
	sparse, err := market.TrackerHistory(ctx, tenant, actor, q)
	if err != nil || len(sparse.Points) != 1 || sparse.Points[0].KnownValue.RatString() != "20" {
		t.Fatal("sparse valuation differs", sparse, err)
	}
	q.ContributionOnly = false
	q.InstrumentKeys = nil
	full, err := market.TrackerHistory(ctx, tenant, actor, q)
	if err != nil {
		t.Fatal(err)
	}
	if full.Points[0].State != "incomplete" {
		t.Fatal("unscoped unknown opening lost its historical warning", full.Points[0])
	}
}
