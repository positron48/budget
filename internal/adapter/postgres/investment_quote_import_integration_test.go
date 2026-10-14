package postgres

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestPortfolioQuoteImportIsAtomicReviewedAndLeavesTheBookUnchanged(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	l, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Tracker", StartedAt: at, PortfolioTracking: true}, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "buy", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "buy", InstrumentKey: "APPLICATION-KEY", Currency: "USD", Quantity: "3", Amount: "30", OccurredAt: at}}); e != nil {
		t.Fatal(e)
	}
	book := func() string {
		t.Helper()
		s, e := svc.Snapshot(ctx, tenant, actor, l.ID, at.Add(time.Minute))
		if e != nil {
			t.Fatal(e)
		}
		b, e := json.Marshal(s)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	before := book()
	makeSeed := func(key string) PortfolioQuoteSeed {
		id := uuid.NewString()
		return PortfolioQuoteSeed{Instrument: md.Instrument{ID: id, Ticker: "CATALOG.ALIAS", Name: "Synthetic", Kind: "share", Currency: "USD", Detailed: true}, Observation: md.Observation{InstrumentID: id, Kind: "last_price", At: at, SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "provider_unspecified", Price: "12.5", Complete: true}, Mapping: md.MappingChange{InstrumentKey: key, RequestKey: "mapping-" + key, Reason: "Reviewed exact identity alias", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: id, Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: at.Truncate(24 * time.Hour), Evidence: "Synthetic identity confirmation"}}}}
	}
	quotes := []PortfolioQuoteSeed{makeSeed("APPLICATION-KEY"), makeSeed("ANOTHER-KEY")}
	for _, apply := range []bool{false, true} {
		bad := append([]PortfolioQuoteSeed(nil), quotes...)
		bad[1].Observation.Price = "0"
		if _, e = repo.ImportPortfolioQuotes(ctx, tenant, actor, bad, PortfolioQuotesSHA256(bad), apply); e == nil {
			t.Fatal("late invalid quote accepted")
		}
		if _, e = repo.GetMapping(ctx, tenant, quotes[0].Mapping.InstrumentKey); !errors.Is(e, md.ErrNotFound) {
			t.Fatal("partial mapping leaked", e)
		}
		var count int
		if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_provider_instruments WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 0 {
			t.Fatal("catalog cache leaked after rollback", count, e)
		}
	}
	sha := PortfolioQuotesSHA256(quotes)
	if _, e = repo.ImportPortfolioQuotes(ctx, tenant, actor, quotes, "wrong", true); e == nil {
		t.Fatal("unchecked input accepted")
	}
	_, other, outsider := acceptanceOwner(t, pool)
	if _, e = repo.ImportPortfolioQuotes(ctx, tenant, outsider, quotes, sha, true); e == nil {
		t.Fatal("cross-tenant actor accepted", other)
	}
	result, e := repo.ImportPortfolioQuotes(ctx, tenant, actor, quotes, sha, false)
	if e != nil || len(result.Quotes) != 2 || result.Quotes[0].CleanPrice.RatString() != "25/2" {
		t.Fatal(result, e)
	}
	if _, e = repo.GetMapping(ctx, tenant, quotes[0].Mapping.InstrumentKey); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("rehearsal committed", e)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, e = repo.ImportPortfolioQuotes(ctx, tenant, actor, quotes, sha, true); e != nil {
			t.Fatal("apply/retry", e)
		}
	}
	expired := append([]PortfolioQuoteSeed(nil), quotes...)
	expired[0].Observation.At = at.Add(-5 * 24 * time.Hour)
	if _, e = repo.ImportPortfolioQuotes(ctx, tenant, actor, expired, PortfolioQuotesSHA256(expired), true); e == nil {
		t.Fatal("fresh cache concealed stale supplied evidence")
	}
	conflict := append([]PortfolioQuoteSeed(nil), quotes...)
	conflict[0].Mapping.RequestKey = "different-review"
	if _, e = repo.ImportPortfolioQuotes(ctx, tenant, actor, conflict, PortfolioQuotesSHA256(conflict), true); !errors.Is(e, md.ErrConflict) {
		t.Fatal("concurrent mapping replaced without its version", e)
	}
	if e = repo.ConfigureSource(ctx, scope, "Synthetic", false); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.ImportPortfolioQuotes(ctx, tenant, actor, quotes, sha, true); e == nil {
		t.Fatal("disabled entitlement revived by retry")
	}
	for _, seed := range quotes {
		m, e := repo.GetMapping(ctx, tenant, seed.Mapping.InstrumentKey)
		if e != nil || m.Version != 1 || m.Periods[0].InstrumentID != seed.Instrument.ID {
			t.Fatal("identity or retry revision changed", m, e)
		}
	}
	if book() != before {
		t.Fatal("quote import changed financial history or quantities")
	}
}
