package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestPortfolioQuoteTracksAmortizedNominalFromCompletedProviderJob(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := repo.ConfigureSource(ctx, scope, "Synthetic exchange", true); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	quoteAt := now.Add(-2 * time.Minute)
	day := quoteAt.Truncate(24 * time.Hour)
	id := uuid.NewString()
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Bond account", StartedAt: day.Add(-48 * time.Hour), PortfolioTracking: true}, "account")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "buy", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "AMORTIZING", Currency: "USD", Quantity: "1", Amount: "1000", OccurredAt: day.Add(-24 * time.Hour)}}); e != nil {
		t.Fatal(e)
	}
	tx, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, scope, md.Instrument{ID: id, Ticker: "TEST", Name: "Synthetic amortizing bond", Kind: "bond", Currency: "RUB", Detailed: true}); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "AMORTIZING", RequestKey: "mapping", Reason: "Original reviewed face value", Periods: []md.MappingPeriod{{Provider: "tinvest", AccessScope: "personal", InstrumentID: id, Kind: "bond", Currency: "USD", Unit: "percent_of_nominal", Nominal: "1000", NominalCurrency: "USD", LotSize: "10", From: day.Add(-24 * time.Hour), Evidence: "Original face value"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := repo.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: id, Kind: "last_price", At: quoteAt, SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "provider_unspecified", Price: "95", Complete: true}}); e != nil {
		t.Fatal(e)
	}
	before, e := repo.InvestmentQuotes(ctx, tenant, actor, []string{"AMORTIZING"})
	if e != nil || before.Quotes[0].CleanPrice != nil {
		t.Fatal("old nominal reused for new-day quote", before, e)
	}
	health, e := repo.ReadMarketWorkerHealth(ctx, scope)
	if e != nil || health.QuoteStates["missing"] != 1 {
		t.Fatal("unconfirmed nominal reported healthy", health, e)
	}
	request := md.JobRequest{Kind: "bond_facts", InstrumentID: id, From: day, To: day.AddDate(0, 0, 30)}
	for attempt := 0; attempt < 2; attempt++ {
		q := request
		q.To = q.To.AddDate(0, 0, attempt)
		if _, e = repo.Enqueue(ctx, scope, q); e != nil {
			t.Fatal(e)
		}
		job, err := repo.Claim(ctx, scope, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		nominal := "800"
		facts := md.BondFacts{Instrument: md.Instrument{ID: id, Ticker: "TEST", Name: "Synthetic amortizing bond", Kind: "bond", Currency: "RUB", Detailed: true}, From: q.From, To: q.To, AsOf: now, Terms: md.BondTerms{Nominal: &md.BondAmount{Amount: nominal, Currency: "USD"}}, Accrued: []md.BondAccrued{{Date: day, Nominal: &nominal}}}
		if attempt == 1 {
			facts.Accrued[0].Nominal = nil
		}
		if e = repo.CompleteBondFacts(ctx, job, facts); e != nil {
			t.Fatal(e)
		}
		actual, err := repo.InvestmentQuotes(ctx, tenant, actor, []string{"AMORTIZING"})
		if err != nil {
			t.Fatal(err)
		}
		if attempt == 0 && (actual.Quotes[0].CleanPrice == nil || actual.Quotes[0].CleanPrice.RatString() != "760" || actual.Quotes[0].Currency != "USD") {
			t.Fatal("amortization/native currency/per-security terms lost", actual)
		}
		if attempt == 1 && actual.Quotes[0].CleanPrice != nil {
			t.Fatal("new incomplete evidence revived earlier nominal", actual)
		}
		health, e = repo.ReadMarketWorkerHealth(ctx, scope)
		if e != nil {
			t.Fatal(e)
		}
		if attempt == 0 && (health.QuoteStates["fresh"] != 1 || health.QuoteStates["missing"] != 0) {
			t.Fatal("usable quote missing from health", health)
		}
		if attempt == 1 && (health.QuoteStates["fresh"] != 0 || health.QuoteStates["missing"] != 1) {
			t.Fatal("completed provider jobs mistaken for usable nominal", health)
		}
	}
	book, e := svc.Snapshot(ctx, tenant, actor, ledger.ID, time.Now().UTC())
	if e != nil || book.Positions[0].CleanCost != "1000" || book.Positions[0].Quantity != "1" {
		t.Fatal("market update changed investment journal", book, e)
	}
	other := scope
	other.AccessScope = "other-entitlement"
	foreign, e := repo.ReadMarketWorkerHealth(ctx, other)
	if e != nil || foreign.QuoteStates["fresh"] != 0 || foreign.QuoteStates["missing"] != 0 {
		t.Fatal("quote health crossed entitlement", foreign, e)
	}
	stored, e := repo.GetMapping(ctx, tenant, "AMORTIZING")
	if e != nil || stored.Version != 1 || stored.Periods[0].Nominal != "1000" {
		t.Fatal("provider cache rewrote reviewed mapping", stored, e)
	}
	gap := stored.Periods[0]
	gap.To = day.Add(-time.Hour)
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "AMORTIZING", ExpectedVersion: 1, RequestKey: "gap", Reason: "Synthetic current terms gap", Periods: []md.MappingPeriod{gap}}); e != nil {
		t.Fatal(e)
	}
	health, e = repo.ReadMarketWorkerHealth(ctx, scope)
	if e != nil || health.QuoteStates["missing"] != 1 {
		t.Fatal("terms gap disappeared from health", health, e)
	}
	plan, e := pool.DB.Query(ctx, "EXPLAIN "+portfolioBondFactsSQL, tenant, scope.Provider, scope.AccessScope, id, day, time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	for plan.Next() {
		var line string
		if e = plan.Scan(&line); e != nil {
			t.Fatal(e)
		}
		t.Log(line)
	}
	e = plan.Err()
	plan.Close()
	if e != nil {
		t.Fatal(e)
	}
}
