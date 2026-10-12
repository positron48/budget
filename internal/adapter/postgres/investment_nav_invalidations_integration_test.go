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

func TestInvestmentNAVDatedInvalidation(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, user string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Dated NAV','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('dated-nav@example.test','test') RETURNING id`).Scan(&user); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, e := svc.CreateLedger(ctx, tenant, user, "Broker", day, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	op := inv.Mutation{Action: "create", RequestKey: "first", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "100", OccurredAt: day.Add(time.Hour)}}
	if _, e = svc.Mutate(ctx, tenant, user, op); e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	q := nav.Request{TenantID: tenant, UserID: user, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market"}
	first, e := repo.BuildDailyNAV(ctx, q, "2024-01-01")
	if e != nil {
		t.Fatal(e)
	}
	second, e := repo.BuildDailyNAV(ctx, q, "2024-01-02")
	if e != nil {
		t.Fatal(e)
	}
	op.RequestKey = "second"
	op.ExpectedLedgerVersion = 2
	op.Operation.Amount = "50"
	op.Operation.OccurredAt = day.Add(25 * time.Hour)
	if _, e = svc.Mutate(ctx, tenant, user, op); e != nil {
		t.Fatal(e)
	}
	history, e := repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-02")
	if e != nil || len(history) != 2 || history[0].State != "ready" || history[0].Result.KnownValue.RatString() != "100" || history[1].State != "stale" || history[1].Result != nil {
		t.Fatal(history, e)
	}
	// Old generation remains publishable for an unaffected date, but not for the
	// affected date. A cached old date is not needlessly invalidated by new input.
	if _, e = repo.publishDailyNAV(ctx, q, "2024-01-01", *first.Result); e != nil {
		t.Fatal("unaffected publication rejected", e)
	}
	if _, e = repo.publishDailyNAV(ctx, q, "2024-01-02", *second.Result); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("affected publication accepted", e)
	}
	if _, e = repo.BuildDailyNAV(ctx, q, "2024-01-01"); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.publishDailyNAV(ctx, q, "2024-01-01", *first.Result); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("projection generation downgraded", e)
	}
	if _, e = repo.BuildDailyNAV(ctx, q, "2024-01-02"); e != nil {
		t.Fatal(e)
	}
	// A quote later than both Moscow cutoffs cannot stale either historical point.
	quote := md.Observation{InstrumentID: uuid.NewString(), Kind: "last_price", At: day.Add(48 * time.Hour), SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "currency_per_unit", Currency: "RUB", Price: "1", Complete: true}
	if _, e = repo.SaveObservations(ctx, md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "public"}, []md.Observation{quote}); e != nil {
		t.Fatal(e)
	}
	history, e = repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-02")
	if e != nil || history[0].State != "ready" || history[1].State != "ready" {
		t.Fatal("recent quote invalidated history", history, e)
	}
	// Re-running older migrations must not replace the dated trigger with the
	// previous global-generation-only function.
	applyMigrations(t, ctx, pool.DB)
	quote.At = quote.At.Add(time.Hour)
	if _, e = repo.SaveObservations(ctx, md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "public"}, []md.Observation{quote}); e != nil {
		t.Fatal(e)
	}
	var current, recorded int64
	if e = pool.DB.QueryRow(ctx, `SELECT g.generation,max(i.generation) FROM investment_valuation_generations g JOIN investment_nav_invalidations i USING(tenant_id) WHERE tenant_id=$1 GROUP BY g.generation`, tenant).Scan(&current, &recorded); e != nil || current != recorded {
		t.Fatal("invalidation lost after migration replay", current, recorded, e)
	}
	// Failed invalidation atomically rejects the journal mutation as well.
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION reject_nav_invalidation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic invalidation failure'; END $$; CREATE TRIGGER reject_nav_invalidation BEFORE INSERT ON investment_nav_invalidations FOR EACH ROW EXECUTE FUNCTION reject_nav_invalidation()`); e != nil {
		t.Fatal(e)
	}
	op.RequestKey = "rollback"
	op.ExpectedLedgerVersion = 3
	op.Operation.OccurredAt = day.Add(2 * time.Hour)
	if _, e = svc.Mutate(ctx, tenant, user, op); e == nil {
		t.Fatal("untracked journal mutation committed")
	}
	var after int64
	if e = pool.DB.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&after); e != nil || after != current {
		t.Fatal(after, e)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER reject_nav_invalidation ON investment_nav_invalidations; DROP FUNCTION reject_nav_invalidation()`); e != nil {
		t.Fatal(e)
	}
	// A retroactive operation invalidates both days, including the previously
	// preserved first day. No stale financial values are returned.
	if _, e = svc.Mutate(ctx, tenant, user, op); e != nil {
		t.Fatal(e)
	}
	history, e = repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-02")
	if e != nil || history[0].Result != nil || history[1].Result != nil {
		t.Fatal(history, e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0028_investment_nav_invalidations")
}
