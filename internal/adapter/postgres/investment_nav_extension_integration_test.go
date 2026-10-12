package postgres

import (
	"context"
	"testing"
	"time"

	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentNAVAutomaticExtension(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Portfolios','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('rolling@example.test','test') RETURNING id`).Scan(&actor); e != nil {
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
	input := nav.PortfolioInput{Name: "Rolling", LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market", ConventionKey: "reviewed", From: "2024-01-01", To: "2024-01-01", RequestKey: "create", AutoExtend: true}
	saved, e := repo.SaveInvestmentPortfolio(ctx, tenant, actor, input)
	if e != nil {
		t.Fatal(e)
	}
	beforeMidnight := time.Date(2024, 1, 2, 20, 59, 59, 0, time.UTC)
	if _, e = repo.scheduleDailyNAVAt(ctx, tenant, beforeMidnight); e != nil {
		t.Fatal(e)
	}
	if worked, e := repo.RunDailyNAVOnce(ctx, tenant); e != nil || !worked {
		t.Fatal(worked, e)
	}
	if n, e := repo.scheduleDailyNAVAt(ctx, tenant, beforeMidnight); e != nil || n != 0 {
		t.Fatal("incomplete Moscow day queued", n, e)
	}
	afterMidnight := beforeMidnight.Add(time.Second)
	type scheduled struct {
		n int
		e error
	}
	results := make(chan scheduled, 2)
	for i := 0; i < 2; i++ {
		go func() { n, e := repo.scheduleDailyNAVAt(ctx, tenant, afterMidnight); results <- scheduled{n, e} }()
	}
	count := 0
	for i := 0; i < 2; i++ {
		result := <-results
		if result.e != nil {
			t.Fatal(result.e)
		}
		count += result.n
	}
	if count != 1 {
		t.Fatal("new date duplicated", count)
	}
	list, e := repo.ListNAVSeries(ctx, tenant, actor, "")
	if e != nil || list[0].To != "2024-01-02" {
		t.Fatal(list, e)
	}
	if worked, e := repo.RunDailyNAVOnce(ctx, tenant); e != nil || !worked {
		t.Fatal(worked, e)
	}
	info, e := repo.GetNAVSeries(ctx, tenant, actor, saved.SeriesKey)
	if e != nil || info.To != "2024-01-02" {
		t.Fatal(info, e)
	}
	if _, e = repo.GetNAVSeries(ctx, tenant, "11111111-1111-4111-8111-111111111111", saved.SeriesKey); e == nil {
		t.Fatal("foreign series exposed")
	}
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION reject_nav_extension() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic extension failure'; END $$; CREATE TRIGGER reject_nav_extension BEFORE INSERT ON investment_nav_jobs FOR EACH ROW EXECUTE FUNCTION reject_nav_extension()`); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.scheduleDailyNAVAt(ctx, tenant, afterMidnight.Add(24*time.Hour)); e == nil {
		t.Fatal("extension failure ignored")
	}
	list, e = repo.ListNAVSeries(ctx, tenant, actor, "")
	if e != nil || list[0].To != "2024-01-02" {
		t.Fatal("range advanced without jobs", list, e)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER reject_nav_extension ON investment_nav_jobs`); e != nil {
		t.Fatal(e)
	}
	input.ID = saved.ID
	input.ExpectedVersion = saved.Version
	input.RequestKey = "archive"
	input.Archived = true
	archived, e := repo.SaveInvestmentPortfolio(ctx, tenant, actor, input)
	if e != nil || archived.To != "2024-01-02" {
		t.Fatal("archive lost extended range", archived, e)
	}
	if n, e := repo.scheduleDailyNAVAt(ctx, tenant, afterMidnight.Add(24*time.Hour)); e != nil || n != 0 {
		t.Fatal("archived range extended", n, e)
	}
	input.ExpectedVersion = archived.Version
	input.RequestKey = "restore"
	input.Archived = false
	if _, e = repo.SaveInvestmentPortfolio(ctx, tenant, actor, input); e != nil {
		t.Fatal(e)
	}
	// Near the safety bound only one day can be added, with an explicit limit flag.
	capDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 3660)
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_nav_watches SET to_date=$2::date,cursor_generation=(SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1) WHERE tenant_id=$1`, tenant, capDate.AddDate(0, 0, -1).Format(time.DateOnly)); e != nil {
		t.Fatal(e)
	}
	if n, e := repo.scheduleDailyNAVAt(ctx, tenant, capDate.AddDate(0, 0, 3)); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	list, e = repo.ListNAVSeries(ctx, tenant, actor, "")
	if e != nil || !list[0].ExtensionLimited || list[0].To != capDate.Format(time.DateOnly) {
		t.Fatal("limit silently ignored", list, e)
	}
	if n, e := repo.scheduleDailyNAVAt(ctx, tenant, capDate.AddDate(0, 0, 4)); e != nil || n != 0 {
		t.Fatal("limit requeued forever", n, e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0035_investment_nav_extension")
}
