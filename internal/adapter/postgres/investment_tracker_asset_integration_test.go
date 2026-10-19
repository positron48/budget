package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/asset"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentTrackerAssetReplacementUsesCleanQuotesWithoutFundingOrDoubleCount(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	ctx = ctxutil.WithUserID(ctxutil.WithTenantID(ctx, tenant), actor)
	assets := asset.NewService(NewAssetRepo(pool), NewFxRepo(pool), NewTenantRepo(pool))
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	create := func(kind string, amount int64) d.AssetAccount {
		t.Helper()
		currency := ""
		if kind == "investment" || kind == "property" || kind == "deposit" {
			currency = "RUB"
		}
		a, e := assets.CreateAccount(ctx, d.AssetAccount{TenantID: tenant, Name: kind, Kind: kind, FixedCurrencyCode: currency}, []d.AssetSnapshot{{UserID: actor, Amount: d.Money{CurrencyCode: "RUB", MinorUnits: amount}, AsOf: at.Add(-time.Hour)}}, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	a := create("investment", 999999)
	bank, property, deposit := create("bank", 200000), create("property", 300000), create("deposit", 400000)
	ledger, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Tracker", StartedAt: at.Add(-time.Hour), PortfolioTracking: true}, "tracker")
	if e != nil {
		t.Fatal(e)
	}
	for i, op := range []d.InvestmentOperation{
		{Kind: "buy", InstrumentKey: "ABC", Currency: "RUB", Quantity: "2", Amount: "200", Fee: "99"},
		{Kind: "buy", InstrumentKey: "UNKNOWN", Currency: "RUB", Quantity: "1", Amount: "50"},
		{Kind: "dividend", InstrumentKey: "ABC", Currency: "RUB", Amount: "123"},
	} {
		op.LedgerID, op.OccurredAt = ledger.ID, at.Add(time.Duration(i)*time.Second)
		if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(i + 1), Operation: op}); e != nil {
			t.Fatal(e)
		}
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e = repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	id := uuid.NewString()
	tx, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, scope, md.Instrument{ID: id, Ticker: "ABC", Name: "Synthetic", Kind: "share", Currency: "USD"}); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "ABC", RequestKey: "map", Reason: "Synthetic", Periods: []md.MappingPeriod{{Provider: "tinvest", AccessScope: "personal", InstrumentID: id, Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: at, Evidence: "Synthetic"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: id, Kind: "last_price", At: at.Add(time.Minute), SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "provider_unspecified", Price: "20", Complete: true}}); e != nil {
		t.Fatal(e)
	}
	if _, e = NewFxRepo(pool).UpsertRate(ctx, "USD", "RUB", "90", at, "manual"); e != nil {
		t.Fatal(e)
	}
	cut := time.Now().UTC().Truncate(time.Microsecond)
	m := d.InvestmentAssetActivation{AccountID: a.ID, LedgerID: ledger.ID, ValuationFrom: cut}
	p, _, e := svc.AssetActivation(ctx, tenant, actor, m, false)
	if e != nil || p.Complete || p.Matches || p.LedgerKnownValue.MinorUnits != 360000 || len(p.Issues) != 1 || p.Issues[0] != "price_missing" {
		t.Fatal("tracker preview must expose known clean value and missing price", p, e)
	}
	m.PreviewHash, m.ExpectedAccountVersion, m.ExpectedLedgerVersion = p.PreviewHash, p.Account.Version, p.Ledger.Version
	m.Evidence, m.RequestKey = "User accepted replacement with explicitly partial quote coverage", "activate-tracker"
	// A rate correction must invalidate even an unchanged rounded preview.
	if _, e = NewFxRepo(pool).UpsertRate(ctx, "USD", "RUB", "90.00000001", at, "manual"); e != nil {
		t.Fatal(e)
	}
	if _, _, e = svc.AssetActivation(ctx, tenant, actor, m, true); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale FX preview accepted", e)
	}
	p, _, e = svc.AssetActivation(ctx, tenant, actor, m, false)
	if e != nil || p.PreviewHash == m.PreviewHash || p.LedgerKnownValue.MinorUnits != 360000 {
		t.Fatal("exact FX evidence lost", p, e)
	}
	m.PreviewHash = p.PreviewHash
	_, linked, e := svc.AssetActivation(ctx, tenant, actor, m, true)
	if e != nil || linked.Balances[0].Amount.MinorUnits != 360000 || !linked.ValuationIncomplete {
		t.Fatal(linked, e)
	}
	_, retried, e := svc.AssetActivation(ctx, tenant, actor, m, true)
	if e != nil || retried.Version != linked.Version {
		t.Fatal("retry", retried, e)
	}
	before, e := NewAssetRepo(pool).GetAccount(ctx, tenant, a.ID, cut.Add(-time.Microsecond))
	if e != nil || before.Balances[0].Amount.MinorUnits != 999999 || before.ValuationStatus != "manual" {
		t.Fatal("historic manual estimate changed", before, e)
	}
	overview, e := assets.GetOverview(ctx, tenant, "RUB", time.Now())
	if e != nil || overview.Total.MinorUnits != 1260000 || !overview.Incomplete || len(overview.Accounts) != 4 {
		t.Fatal("double count or incomplete valuation hidden", overview, e)
	}
	for _, original := range []d.AssetAccount{bank, property, deposit} {
		current, e := NewAssetRepo(pool).GetAccount(ctx, tenant, original.ID, time.Now())
		if e != nil || current.Version != original.Version || current.Balances[0].Amount != original.Balances[0].Amount {
			t.Fatal("ordinary asset changed", current, e)
		}
	}
	if _, e = assets.CreateSnapshot(ctx, d.AssetSnapshot{TenantID: tenant, AccountID: a.ID, UserID: actor, Kind: "valuation", Amount: d.Money{CurrencyCode: "RUB", MinorUnits: 1}, AsOf: time.Now()}, "manual-bypass"); !errors.Is(e, d.ErrAssetPrecondition) {
		t.Fatal("manual value bypass", e)
	}
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: id, Kind: "last_price", At: time.Now().UTC().Add(-time.Millisecond).Truncate(time.Microsecond), SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "provider_unspecified", Price: "21", Complete: true}}); e != nil {
		t.Fatal(e)
	}
	updated, e := NewAssetRepo(pool).GetAccount(ctx, tenant, a.ID, time.Now())
	if e != nil || updated.Balances[0].Amount.MinorUnits != 378000 || updated.Version != linked.Version {
		t.Fatal("new quote did not update value without a manual snapshot", updated, e)
	}
	stale, e := NewAssetRepo(pool).GetAccount(ctx, tenant, a.ID, time.Now().Add(5*24*time.Hour))
	if e != nil || !stale.ValuationIncomplete || stale.Balances[0].Amount.MinorUnits != 378000 {
		t.Fatal("last-known quote lost value or revived the replaced manual balance", stale, e)
	}
}
