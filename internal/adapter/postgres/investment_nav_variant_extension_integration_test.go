package postgres

import (
	"context"
	"github.com/google/uuid"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestInvestmentVariantInheritsRollingScopeAndStopsAfterArchive(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	if _, e := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Variants','RUB')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Rolling", at, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	other, e := svc.CreateLedger(ctx, tenant, actor, "Other", at, "other")
	if e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	for _, key := range []string{"reviewed", "different"} {
		if _, e = repo.RegisterPriceConventions(ctx, tenant, actor, nav.ConventionProfile{Key: key, Title: key, Evidence: "Synthetic", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{}}, 0); e != nil {
			t.Fatal(e)
		}
	}
	for _, mode := range []string{"market", "cbr"} {
		if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, md.FXPolicy{Key: mode, Mode: mode, Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}, 0); e != nil {
			t.Fatal(e)
		}
	}
	input := nav.PortfolioInput{Name: "Rolling", LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market", ConventionKey: "reviewed", From: "2024-01-01", To: "2024-01-01", RequestKey: "create", AutoExtend: true}
	portfolio, e := repo.SaveInvestmentPortfolio(ctx, tenant, actor, input)
	if e != nil {
		t.Fatal(e)
	}
	usd, e := repo.PrepareNAVVariant(ctx, tenant, actor, portfolio.SeriesKey, "USD", "market")
	if e != nil {
		t.Fatal(e)
	}
	eur, e := repo.PrepareNAVVariant(ctx, tenant, actor, portfolio.SeriesKey, "EUR", "cbr")
	if e != nil {
		t.Fatal(e)
	}
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: input.LedgerIDs, Currency: "EUR", FXPolicyKey: "market", ConventionKey: "different"}
	if _, e = repo.EnqueueDailyNAV(ctx, q, input.From, input.To); e != nil {
		t.Fatal(e)
	}
	differentKey, _, _ := navSeries(q)
	q.ConventionKey = "reviewed"
	q.LedgerIDs = []string{other.ID}
	if _, e = repo.EnqueueDailyNAV(ctx, q, input.From, input.To); e != nil {
		t.Fatal(e)
	}
	otherKey, _, _ := navSeries(q)
	if _, e = repo.scheduleDailyNAVAt(ctx, tenant, at.Add(48*time.Hour)); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{portfolio.SeriesKey, usd.Series.Key, eur.Series.Key} {
		summary, e := repo.GetNAVSeries(ctx, tenant, actor, key)
		if e != nil || summary.To != "2024-01-02" {
			t.Fatal("currency/FX variant failed to roll", summary, e)
		}
	}
	for _, key := range []string{differentKey, otherKey} {
		summary, e := repo.GetNAVSeries(ctx, tenant, actor, key)
		if e != nil || summary.To != "2024-01-01" {
			t.Fatal("different scope or rules rolled", summary, e)
		}
	}
	input.ID = portfolio.ID
	input.ExpectedVersion = portfolio.Version
	input.Archived = true
	input.RequestKey = "archive"
	if _, e = repo.SaveInvestmentPortfolio(ctx, tenant, actor, input); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.scheduleDailyNAVAt(ctx, tenant, at.Add(72*time.Hour)); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{usd.Series.Key, eur.Series.Key} {
		summary, e := repo.GetNAVSeries(ctx, tenant, actor, key)
		if e != nil || summary.To != "2024-01-02" {
			t.Fatal("archived source kept rolling", summary, e)
		}
	}
}
