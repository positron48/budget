package postgres

import (
	"context"
	"errors"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"testing"
	"time"
)

func TestInvestmentNAVJobsRecovery(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('NAV jobs','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('nav-jobs@example.test','test') RETURNING id`).Scan(&actor); e != nil {
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
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market"}
	if n, e := repo.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-02"); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	if n, e := repo.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-02"); e != nil || n != 0 {
		t.Fatal("active dedup", n, e)
	}
	first, e := repo.claimDailyNAV(ctx, tenant)
	if e != nil || first.ID == "" {
		t.Fatal(first, e)
	}
	second, e := repo.claimDailyNAV(ctx, tenant)
	if e != nil || second.ID == "" || second.ID == first.ID {
		t.Fatal(second, e)
	}
	if j, e := repo.claimDailyNAV(ctx, tenant); e != nil || j.ID != "" {
		t.Fatal("claimed live lease", j, e)
	}
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_nav_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, first.ID); e != nil {
		t.Fatal(e)
	}
	recovered, e := repo.claimDailyNAV(ctx, tenant)
	if e != nil || recovered.ID != first.ID || recovered.Token == first.Token {
		t.Fatal("restart recovery", recovered, e)
	}
	if e = repo.finishDailyNAV(ctx, first, nil); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("old lease completed", e)
	}
	if _, e = repo.BuildDailyNAV(ctx, recovered.Request, recovered.Date); e != nil {
		t.Fatal(e)
	}
	// Simulated crash after publication: next worker must recover idempotently.
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_nav_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, recovered.ID); e != nil {
		t.Fatal(e)
	}
	if worked, e := repo.RunDailyNAVOnce(ctx, tenant); e != nil || !worked {
		t.Fatal(worked, e)
	}
	var revisions int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_daily_nav_revisions WHERE tenant_id=$1`, tenant).Scan(&revisions); e != nil || revisions != 1 {
		t.Fatal("duplicate revision", revisions, e)
	}
	// Revocation is checked again by the worker, not only when scheduling.
	if _, e = pool.DB.Exec(ctx, `DELETE FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_nav_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, second.ID); e != nil {
		t.Fatal(e)
	}
	if worked, e := repo.RunDailyNAVOnce(ctx, tenant); !worked || !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("revocation ignored", worked, e)
	}
	var status, code string
	if e = pool.DB.QueryRow(ctx, `SELECT status,error_code FROM investment_nav_jobs WHERE id=$1`, second.ID).Scan(&status, &code); e != nil || status != "error" || code != "request_rejected" {
		t.Fatal(status, code, e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0029_investment_nav_jobs")
}
