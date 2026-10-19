package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentQuotesOutsideBrokerHoldingsAndSourceBoundaries(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := repo.ConfigureSource(ctx, scope, "Synthetic exchange source", true); e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for _, key := range []string{"STOCK", "BOND", "STALE"} {
		kind, unit, nominal := "share", "currency_per_unit", ""
		if key == "BOND" {
			kind, unit, nominal = "bond", "percent_of_nominal", "500"
		}
		item := md.Instrument{ID: uuid.NewString(), Ticker: key, Name: key, ClassCode: "SYNTHETIC", Kind: kind, Currency: "USD"}
		tx, e := pool.DB.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = saveMarketInstrument(ctx, tx, scope, item); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		change := md.MappingChange{InstrumentKey: key, RequestKey: key, Reason: "Synthetic current price terms", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: item.ID, Kind: kind, Currency: "USD", Unit: unit, Nominal: nominal, NominalCurrency: "USD", LotSize: "100", From: at.Add(-10 * 24 * time.Hour), Evidence: "Synthetic metadata"}}}
		if kind == "bond" {
			change.Periods[0].From = at.UTC().Truncate(24 * time.Hour)
		}
		if key == "STALE" {
			change.Periods[0].From = at.Add(-100 * 24 * time.Hour)
		}
		if _, e = repo.SaveMapping(ctx, tenant, actor, change); e != nil {
			t.Fatal(e)
		}
		quoteAt := at
		if key == "STALE" {
			quoteAt = at.Add(-70 * 24 * time.Hour)
		}
		quote := md.Observation{InstrumentID: item.ID, Kind: "last_price", At: quoteAt, SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "provider_unspecified", Price: "95", Complete: true}
		if _, e = repo.SaveObservations(ctx, scope, []md.Observation{quote}); e != nil {
			t.Fatal(e)
		}
		// A newer dealer quote and future exchange quote must not replace the eligible series.
		quote.SourceKind, quote.At, quote.Price = "dealer", at.Add(time.Minute), "999"
		if _, e = repo.SaveObservations(ctx, scope, []md.Observation{quote}); e != nil {
			t.Fatal(e)
		}
		quote.SourceKind, quote.At = "exchange", at.Add(2*time.Hour)
		if _, e = repo.SaveObservations(ctx, scope, []md.Observation{quote}); e != nil {
			t.Fatal(e)
		}
	}
	keys := []string{"STOCK", "BOND", "STALE", "MISSING"}
	result, e := repo.InvestmentQuotes(ctx, tenant, actor, keys)
	if e != nil || len(result.Quotes) != 4 {
		t.Fatal(result, e)
	}
	for _, q := range result.Quotes {
		if q.InstrumentKey != "MISSING" && (q.Instrument == nil || q.Instrument.Ticker != q.InstrumentKey || q.MetadataAt.IsZero()) {
			t.Fatal("provider metadata missing", q)
		}
		if q.InstrumentKey == "BOND" && (q.RawPrice == nil || q.RawPrice.RatString() != "95" || q.RawUnit != "percent_of_nominal" || q.Nominal == nil || q.Nominal.RatString() != "500" || q.Accrued != nil) {
			t.Fatal("bond quote details incorrect", q)
		}
		if q.InstrumentKey == "STOCK" && (q.CleanPrice == nil || q.CleanPrice.RatString() != "95") {
			t.Fatal(q)
		}
		if q.InstrumentKey == "BOND" && (q.CleanPrice == nil || q.CleanPrice.RatString() != "475") {
			t.Fatal(q)
		}
		if q.InstrumentKey == "STALE" && (q.CleanPrice == nil || q.CleanPrice.RatString() != "95" || q.State != "estimated" || !q.At.Equal(at.Add(-70*24*time.Hour))) {
			t.Fatal("last-known quote was lost or given a new date", q)
		}
		if q.InstrumentKey == "MISSING" && q.CleanPrice != nil {
			t.Fatal("missing quote became money", q)
		}
	}
	// No broker snapshots, journals or NAV registrations were created.
	var count int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
	if _, e = repo.InvestmentQuotes(ctx, uuid.NewString(), actor, keys); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("tenant boundary", e)
	}
	plan, e := pool.DB.Query(ctx, `EXPLAIN SELECT instrument_key,data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=ANY($2::text[])`, tenant, keys)
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
	if e = repo.ConfigureSource(ctx, scope, "Synthetic exchange source", false); e != nil {
		t.Fatal(e)
	}
	result, e = repo.InvestmentQuotes(ctx, tenant, actor, keys)
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range result.Quotes {
		if q.CleanPrice != nil || q.Instrument != nil {
			t.Fatal("disabled source leaked", q)
		}
	}
}
