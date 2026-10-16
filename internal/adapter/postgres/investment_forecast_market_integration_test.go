package postgres

import (
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestInvestmentForecastReadsMappedProviderCouponsWithoutSavingEventsOrOperations(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	market := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := market.ConfigureSource(ctx, scope, "Synthetic coupon source", true); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Second)
	start := now.AddDate(0, -1, 0)
	pay := now.AddDate(0, 0, 20)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Bond tracker", StartedAt: start, PortfolioTracking: true}, "account")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "buy", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "BOND", Currency: "USD", Quantity: "2", Amount: "100", OccurredAt: start.Add(time.Hour)}}); e != nil {
		t.Fatal(e)
	}
	id := uuid.NewString()
	request := md.JobRequest{Kind: "bond_facts", InstrumentID: id, From: now.Add(-time.Hour), To: now.AddDate(0, 0, 364)}
	if _, e = market.Enqueue(ctx, scope, request); e != nil {
		t.Fatal(e)
	}
	job, e := market.Claim(ctx, scope, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	facts := md.BondFacts{Instrument: md.Instrument{ID: id, Ticker: "BOND", Name: "Synthetic bond", Kind: "bond", Currency: "USD", Detailed: true}, From: request.From, To: request.To, AsOf: now, Coupons: []md.BondCoupon{{Number: 1, Date: pay, Kind: "COUPON_TYPE_FIX", Pay: &md.BondAmount{Currency: "USD", Amount: "5"}}}}
	if e = market.CompleteBondFacts(ctx, job, facts); e != nil {
		t.Fatal(e)
	}
	periods := []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: id, Kind: "bond", Currency: "USD", Unit: "percent_of_nominal", Nominal: "100", NominalCurrency: "USD", From: start, Evidence: "Synthetic approved mapping"}}
	if _, e = market.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "BOND", RequestKey: "mapping", Reason: "Synthetic mapping", Periods: periods}); e != nil {
		t.Fatal(e)
	}
	// Older imported mappings encode absent current periods as JSON null.
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_market_mappings SET data=jsonb_set(jsonb_set(data,'{HistoricalPeriods}',data->'Periods'),'{Periods}','null'::jsonb) WHERE tenant_id=$1`, tenant); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	q := inv.ForecastQuery{LedgerIDs: []string{ledger.ID}, FromMonth: now.Format("2006-01"), Currency: "USD"}
	result, e := repo.Forecast(ctx, tenant, actor, q)
	if e != nil || len(result.Rows) != 1 || result.Rows[0].Gross == nil || result.Rows[0].Gross.RatString() != "10" || len(result.Events) != 1 || result.Events[0].Origin != "provider" {
		t.Fatal(e, result)
	}
	var operations, events int
	if e = pool.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM investment_operations WHERE tenant_id=$1),(SELECT count(*) FROM investment_payout_events WHERE tenant_id=$1)`, tenant).Scan(&operations, &events); e != nil || operations != 1 || events != 0 {
		t.Fatal("forecast wrote financial or saved evidence", e, operations, events)
	}
	if e = market.ConfigureSource(ctx, scope, "Synthetic disabled source", false); e != nil {
		t.Fatal(e)
	}
	result, e = repo.Forecast(ctx, tenant, actor, q)
	if e != nil || len(result.Rows) != 0 {
		t.Fatal("disabled source leaked", e, result)
	}
}
