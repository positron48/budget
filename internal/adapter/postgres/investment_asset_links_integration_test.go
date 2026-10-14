package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/asset"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

func TestTrackerAssetBatchIsAtomicRehearsedAndResumable(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	ctx = ctxutil.WithUserID(ctxutil.WithTenantID(ctx, tenant), actor)
	ar := NewAssetRepo(pool)
	assets := asset.NewService(ar, NewFxRepo(pool), NewTenantRepo(pool))
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	create := func(name, kind string, minor int64) d.AssetAccount {
		t.Helper()
		currency := ""
		if kind == "investment" || kind == "property" || kind == "deposit" {
			currency = "RUB"
		}
		a, e := assets.CreateAccount(ctx, d.AssetAccount{TenantID: tenant, Name: name, Kind: kind, FixedCurrencyCode: currency}, []d.AssetSnapshot{{UserID: actor, Amount: d.Money{CurrencyCode: "RUB", MinorUnits: minor}, AsOf: at}}, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	bank := create("Ordinary", "bank", 200000)
	links := []TrackerAssetLink{}
	for i := 0; i < 6; i++ {
		a := create("Investment "+string(rune('A'+i)), "investment", int64(i+1)*10000)
		l, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: a.Name, StartedAt: at, PortfolioTracking: true}, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		links = append(links, TrackerAssetLink{AccountID: a.ID, AccountName: a.Name, LedgerID: l.ID, LedgerName: l.Name, Currency: "RUB", ExpectedAccountVersion: a.Version, ExpectedManualMinor: int64(i+1) * 10000})
	}
	// A real holding with no quote must remain explicitly partial, while empty
	// portfolios are zero-valued. The batch must never invent quotes or funding.
	if _, e := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: links[0].LedgerID, OccurredAt: at.Add(time.Minute), Kind: "buy", InstrumentKey: "UNPRICED", Currency: "RUB", Quantity: "2", Amount: "100"}}); e != nil {
		t.Fatal(e)
	}
	var journalBefore string
	if e := pool.DB.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY id)::text,'[]') FROM investment_operations o WHERE tenant_id=$1`, tenant).Scan(&journalBefore); e != nil {
		t.Fatal(e)
	}
	unchanged := func() {
		t.Helper()
		for _, l := range links {
			a, e := ar.GetAccount(ctx, tenant, l.AccountID, time.Now())
			if e != nil || a.InvestmentMode != "manual" || a.Version != l.ExpectedAccountVersion {
				t.Fatal("partial activation escaped rollback", a, e)
			}
		}
	}
	bad := append([]TrackerAssetLink(nil), links...)
	bad[5].ExpectedManualMinor++
	if _, e := repo.LinkTrackerAssets(ctx, tenant, actor, bad, TrackerAssetLinksSHA256(bad), true); e == nil {
		t.Fatal("late changed manual value accepted")
	}
	unchanged()
	if _, e := repo.LinkTrackerAssets(ctx, tenant, actor, links, "wrong", true); e == nil {
		t.Fatal("unchecked batch accepted")
	}
	unchanged()
	_, _, outsider := acceptanceOwner(t, pool)
	if _, e := repo.LinkTrackerAssets(ctx, tenant, outsider, links, TrackerAssetLinksSHA256(links), true); e == nil {
		t.Fatal("outsider accepted")
	}
	unchanged()
	r, e := repo.LinkTrackerAssets(ctx, tenant, actor, links, TrackerAssetLinksSHA256(links), false)
	if e != nil || len(r) != 6 || r[0].Complete || !r[1].Complete {
		t.Fatal("real rehearsal", r, e)
	}
	unchanged()
	var audit int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_asset_activations WHERE tenant_id=$1`, tenant).Scan(&audit); e != nil || audit != 0 {
		t.Fatal("rehearsal audit committed", audit, e)
	}
	beforeCut := time.Now().UTC()
	for retry := 0; retry < 2; retry++ {
		r, e = repo.LinkTrackerAssets(ctx, tenant, actor, links, TrackerAssetLinksSHA256(links), true)
		if e != nil {
			t.Fatal(e)
		}
		for _, x := range r {
			if x.Reused != (retry == 1) || x.SecuritiesMinor != 0 {
				t.Fatal("retry or missing quote value", x)
			}
		}
	}
	for _, l := range links {
		a, e := ar.GetAccount(ctx, tenant, l.AccountID, beforeCut)
		if e != nil || a.Balances[0].Amount.MinorUnits != l.ExpectedManualMinor || a.ValuationStatus != "manual" {
			t.Fatal("manual history changed", a, e)
		}
		a, e = ar.GetAccount(ctx, tenant, l.AccountID, time.Now())
		if e != nil || a.Version != l.ExpectedAccountVersion+1 || a.InvestmentLedgerID != l.LedgerID {
			t.Fatal("link mismatch", a, e)
		}
	}
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_asset_activations WHERE tenant_id=$1`, tenant).Scan(&audit); e != nil || audit != 6 {
		t.Fatal("duplicate or missing audit", audit, e)
	}
	b, e := ar.GetAccount(ctx, tenant, bank.ID, time.Now())
	if e != nil || b.Balances[0].Amount.MinorUnits != 200000 || b.Version != bank.Version {
		t.Fatal("ordinary balance changed", b, e)
	}
	var journalAfter string
	if e = pool.DB.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY id)::text,'[]') FROM investment_operations o WHERE tenant_id=$1`, tenant).Scan(&journalAfter); e != nil || journalAfter != journalBefore {
		t.Fatal("operations changed", e)
	}
}
