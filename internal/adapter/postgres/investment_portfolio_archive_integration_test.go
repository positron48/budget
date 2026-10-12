package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentPortfolioArchiveOwnership(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Portfolios','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('archives@example.test','test') RETURNING id`).Scan(&actor); e != nil {
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
	input := nav.PortfolioInput{Name: "First", LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market", ConventionKey: "reviewed", From: "2024-01-01", To: "2024-01-03", RequestKey: "first"}
	first, e := repo.SaveInvestmentPortfolio(ctx, tenant, actor, input)
	if e != nil {
		t.Fatal(e)
	}
	input.Name = "Second"
	input.RequestKey = "second"
	second, e := repo.SaveInvestmentPortfolio(ctx, tenant, actor, input)
	if e != nil || second.SeriesKey != first.SeriesKey {
		t.Fatal(second, e)
	}
	running, e := repo.claimDailyNAV(ctx, tenant)
	if e != nil || running.ID == "" {
		t.Fatal(running, e)
	}
	if _, e = repo.BuildDailyNAV(ctx, running.Request, running.Date); e != nil {
		t.Fatal(e)
	}
	archive := func(p nav.Portfolio, key string) (nav.Portfolio, error) {
		return repo.SaveInvestmentPortfolio(ctx, tenant, actor, nav.PortfolioInput{ID: p.ID, Name: p.Name, LedgerIDs: p.LedgerIDs, Currency: p.Currency, FXPolicyKey: p.FXPolicyKey, ConventionKey: p.ConventionKey, From: p.From, To: p.To, ExpectedVersion: p.Version, RequestKey: key, Archived: true})
	}
	first, e = archive(first, "archive-first")
	if e != nil || !first.Archived {
		t.Fatal(first, e)
	}
	var enabled bool
	if e = pool.DB.QueryRow(ctx, `SELECT enabled FROM investment_nav_watches WHERE tenant_id=$1 AND series_key=$2`, tenant, first.SeriesKey).Scan(&enabled); e != nil || !enabled {
		t.Fatal("shared subscription stopped", enabled, e)
	}
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION reject_nav_cancel() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='cancelled' THEN RAISE EXCEPTION 'synthetic cancellation failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_nav_cancel BEFORE UPDATE ON investment_nav_jobs FOR EACH ROW EXECUTE FUNCTION reject_nav_cancel()`); e != nil {
		t.Fatal(e)
	}
	if _, e = archive(second, "archive-second"); e == nil {
		t.Fatal("partial archive accepted")
	}
	if e = pool.DB.QueryRow(ctx, `SELECT enabled FROM investment_nav_watches WHERE tenant_id=$1 AND series_key=$2`, tenant, first.SeriesKey).Scan(&enabled); e != nil || !enabled {
		t.Fatal("watch changed after rollback", enabled, e)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER reject_nav_cancel ON investment_nav_jobs`); e != nil {
		t.Fatal(e)
	}
	second, e = archive(second, "archive-second")
	if e != nil {
		t.Fatal(e)
	}
	if e = repo.finishDailyNAV(ctx, running, nil); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("cancelled worker finished", e)
	}
	var jobs int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_nav_jobs WHERE tenant_id=$1 AND status='cancelled'`, tenant).Scan(&jobs); e != nil || jobs != 3 {
		t.Fatal(jobs, e)
	}
	if n, e := repo.ScheduleDailyNAV(ctx, tenant); e != nil || n != 0 {
		t.Fatal("archive rescheduled", n, e)
	}
	if list, e := repo.ListNAVSeries(ctx, tenant, actor, ""); e != nil || len(list) != 0 {
		t.Fatal(list, e)
	}
	if points, e := repo.NAVHistoryForSeries(ctx, tenant, actor, first.SeriesKey, running.Date, running.Date); e != nil || points[0].Result == nil {
		t.Fatal("archive history lost", points, e)
	}
	if e = repo.RequestNAVRefresh(ctx, tenant, actor, first.SeriesKey); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("archived refresh accepted", e)
	}
	input.ID = first.ID
	input.Name = first.Name
	input.ExpectedVersion = first.Version
	input.RequestKey = "restore"
	restored, e := repo.SaveInvestmentPortfolio(ctx, tenant, actor, input)
	if e != nil || restored.Archived {
		t.Fatal(restored, e)
	}
	if list, e := repo.ListNAVSeries(ctx, tenant, actor, ""); e != nil || len(list) != 1 {
		t.Fatal("restore did not reactivate", list, e)
	}
	// Moving the last live consumer to another series retires its old watch.
	input.ExpectedVersion = restored.Version
	input.RequestKey = "change-currency"
	input.Currency = "USD"
	changed, e := repo.SaveInvestmentPortfolio(ctx, tenant, actor, input)
	if e != nil {
		t.Fatal(e)
	}
	if e = pool.DB.QueryRow(ctx, `SELECT enabled FROM investment_nav_watches WHERE tenant_id=$1 AND series_key=$2`, tenant, first.SeriesKey).Scan(&enabled); e != nil || enabled {
		t.Fatal("orphan watch still enabled", enabled, e)
	}
	// Explicit internal consumers have independent ownership, retained on archive.
	if _, e = repo.EnqueueDailyNAV(ctx, nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: changed.LedgerIDs, Currency: changed.Currency, FXPolicyKey: changed.FXPolicyKey, ConventionKey: changed.ConventionKey}, changed.From, changed.To); e != nil {
		t.Fatal(e)
	}
	if _, e = archive(changed, "archive-with-internal-owner"); e != nil {
		t.Fatal(e)
	}
	if e = pool.DB.QueryRow(ctx, `SELECT enabled FROM investment_nav_watches WHERE tenant_id=$1 AND series_key=$2`, tenant, changed.SeriesKey).Scan(&enabled); e != nil || !enabled {
		t.Fatal("internal subscription removed", enabled, e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0034_investment_portfolio_archive")
}
