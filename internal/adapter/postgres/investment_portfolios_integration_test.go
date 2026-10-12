package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentPortfolioAtomicSave(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Portfolios','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('portfolios@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "ledger")
	if e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	profile := nav.ConventionProfile{Key: "reviewed", Title: "Reviewed rules", Evidence: "Synthetic", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{}}
	if _, e = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0); e != nil {
		t.Fatal(e)
	}
	fx := md.FXPolicy{Key: "market", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, fx, 0); e != nil {
		t.Fatal(e)
	}
	input := nav.PortfolioInput{Name: " My portfolio ", LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market", ConventionKey: "reviewed", From: "2024-01-01", To: "2024-01-03", RequestKey: "create"}
	// Two simultaneous retries must produce one definition, audit and job set.
	type result struct {
		p nav.Portfolio
		e error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() { p, e := repo.SaveInvestmentPortfolio(ctx, tenant, actor, input); results <- result{p, e} }()
	}
	first := <-results
	second := <-results
	if first.e != nil || second.e != nil || first.p.ID != second.p.ID || first.p.Version != 1 || first.p.Name != "My portfolio" {
		t.Fatal(first, second)
	}
	var count int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_nav_jobs WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 3 {
		t.Fatal(count, e)
	}
	for i := 0; i < 3; i++ {
		if worked, e := repo.RunDailyNAVOnce(ctx, tenant); e != nil || !worked {
			t.Fatal(worked, e)
		}
	}
	history, e := repo.NAVHistoryForSeries(ctx, tenant, actor, first.p.SeriesKey, input.From, input.To)
	if e != nil || len(history) != 3 || history[0].State != "ready" {
		t.Fatal(history, e)
	}
	options, e := repo.InvestmentPortfolioOptions(ctx, tenant, actor, "", "")
	if e != nil || len(options.Price) != 1 || len(options.FX) != 1 || options.FX[0].Mode != "market" {
		t.Fatal(options, e)
	}
	list, e := repo.ListInvestmentPortfolios(ctx, tenant, actor, "")
	if e != nil || len(list) != 1 || list[0].ID != first.p.ID {
		t.Fatal(list, e)
	}
	if list, e = repo.ListInvestmentPortfolios(ctx, tenant, actor, first.p.ID); e != nil || len(list) != 0 {
		t.Fatal(list, e)
	}
	if list, e = repo.ListInvestmentPortfolios(ctx, uuid.NewString(), actor, ""); e != nil || len(list) != 0 {
		t.Fatal("foreign tenant exposed", list, e)
	}
	input.ID = first.p.ID
	input.ExpectedVersion = 1
	input.RequestKey = "rename"
	input.Name = "Renamed"
	renamed, e := repo.SaveInvestmentPortfolio(ctx, tenant, actor, input)
	if e != nil || renamed.Version != 2 || renamed.SeriesKey != first.p.SeriesKey {
		t.Fatal(renamed, e)
	}
	input.RequestKey = "old-version"
	if _, e = repo.SaveInvestmentPortfolio(ctx, tenant, actor, input); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale overwrite", e)
	}
	input.ExpectedVersion = 2
	input.RequestKey = "foreign-ledger"
	input.LedgerIDs = []string{uuid.NewString()}
	if _, e = repo.SaveInvestmentPortfolio(ctx, tenant, actor, input); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("foreign ledger accepted", e)
	}
	input.LedgerIDs = []string{ledger.ID}
	input.From = "2023-12-31"
	input.RequestKey = "extend-before-coverage"
	extended, e := repo.SaveInvestmentPortfolio(ctx, tenant, actor, input)
	if e != nil || extended.Version != 3 {
		t.Fatal(extended, e)
	}
	point, e := repo.BuildDailyNAV(ctx, nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: input.LedgerIDs, Currency: input.Currency, ConventionKey: input.ConventionKey, FXPolicyKey: input.FXPolicyKey}, "2023-12-31")
	if e != nil || point.State != "incomplete" || len(point.Result.Issues) != 1 || point.Result.Issues[0].Code != "ledger_history_missing" {
		t.Fatal("pre-coverage history invented", point, e)
	}
	input.ExpectedVersion = 3
	input.From = "2024-01-01"
	input.RequestKey = "atomic-failure"
	input.Name = "Must roll back"
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION reject_portfolio_jobs() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic queue failure'; END $$; CREATE TRIGGER reject_portfolio_jobs BEFORE INSERT ON investment_nav_jobs FOR EACH ROW EXECUTE FUNCTION reject_portfolio_jobs()`); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveInvestmentPortfolio(ctx, tenant, actor, input); e == nil {
		t.Fatal("queue failure ignored")
	}
	list, e = repo.ListInvestmentPortfolios(ctx, tenant, actor, "")
	if e != nil || list[0].Name != "Renamed" || list[0].Version != 3 {
		t.Fatal("partial portfolio saved", list, e)
	}
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_portfolio_requests WHERE tenant_id=$1 AND request_key='atomic-failure'`, tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("failed request committed", count, e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0033_investment_portfolios")
}
