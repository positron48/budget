package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

func TestInvestmentTrackerSelectionsOwnerRestoreAndNoJobs(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	book := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	market := NewMarketDataRepo(pool)
	ids := []string{}
	for _, name := range []string{"First", "Second"} {
		l, err := book.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: name, StartedAt: time.Now(), PortfolioTracking: true}, uuid.NewString())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, l.ID)
	}
	input := nav.PortfolioInput{TrackerSelection: true, Name: "Two brokers", LedgerIDs: ids, RequestKey: "selection"}
	saved, err := market.SaveInvestmentPortfolio(ctx, tenant, actor, input)
	if err != nil || !saved.TrackerSelection || saved.SeriesKey != "" || saved.Version != 1 {
		t.Fatal(saved, err)
	}
	retry, err := market.SaveInvestmentPortfolio(ctx, tenant, actor, input)
	if err != nil || retry.ID != saved.ID || retry.Version != 1 {
		t.Fatal("retry", retry, err)
	}
	var member string
	if err = pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id`, uuid.NewString()+"@example.test").Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'member')`, tenant, member); err != nil {
		t.Fatal(err)
	}
	private, err := market.ListInvestmentPortfolios(ctx, tenant, member, "")
	if err != nil || len(private) != 0 {
		t.Fatal("actor scope leaked", private, err)
	}
	edit := input
	edit.ID = saved.ID
	edit.ExpectedVersion = 1
	edit.RequestKey = "edit"
	edit.LedgerIDs = ids[:1]
	if _, err = market.SaveInvestmentPortfolio(ctx, tenant, member, edit); !errors.Is(err, d.ErrInvestmentNotFound) {
		t.Fatal("actor scope write", err)
	}
	own, err := market.SaveInvestmentPortfolio(ctx, tenant, member, input)
	if err != nil || !own.TrackerSelection || own.ID == saved.ID {
		t.Fatal("member cannot save private selection", own, err)
	}
	saved, err = market.SaveInvestmentPortfolio(ctx, tenant, actor, edit)
	if err != nil || saved.Version != 2 || len(saved.LedgerIDs) != 1 {
		t.Fatal(saved, err)
	}
	edit.RequestKey = "stale"
	if _, err = market.SaveInvestmentPortfolio(ctx, tenant, actor, edit); !errors.Is(err, d.ErrInvestmentConflict) {
		t.Fatal("lost update", err)
	}
	invalid := input
	invalid.RequestKey = "foreign"
	invalid.LedgerIDs = []string{uuid.NewString()}
	if _, err = market.SaveInvestmentPortfolio(ctx, tenant, actor, invalid); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("foreign account accepted", err)
	}
	for _, table := range []string{"investment_nav_jobs", "investment_nav_watches", "investment_nav_series"} {
		var n int
		if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, tenant).Scan(&n); err != nil || n != 0 {
			t.Fatal("selection created derived work", table, n, err)
		}
	}
	archive, err := NewInvestmentRepo(pool).ExportInvestmentArchive(ctx, tenant, actor)
	if err != nil {
		t.Fatal(err)
	}
	target, cleanup := withPg(t)
	defer cleanup()
	if _, err = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restored','RUB')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'selection-restore@example.test','fixture')`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'selection-member-restore@example.test','fixture')`, member); err != nil {
		t.Fatal(err)
	}
	if _, err = NewInvestmentRepo(target).RestoreInvestmentArchive(ctx, actor, archive, archive.SHA256); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("active selection restored without actor membership", err)
	}
	var partial int
	if err = target.DB.QueryRow(ctx, `SELECT count(*) FROM investment_ledgers`).Scan(&partial); err != nil || partial != 0 {
		t.Fatal("failed restore left partial data", partial, err)
	}
	if _, err = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'member')`, tenant, member); err != nil {
		t.Fatal(err)
	}
	if _, err = NewInvestmentRepo(target).RestoreInvestmentArchive(ctx, actor, archive, archive.SHA256); err != nil {
		t.Fatal("restore", err)
	}
	restored, err := NewMarketDataRepo(target).ListInvestmentPortfolios(ctx, tenant, actor, "")
	if err != nil || len(restored) != 1 || !restored[0].TrackerSelection || restored[0].ID != saved.ID || restored[0].Version != 2 {
		t.Fatal(restored, err)
	}
	for _, table := range []string{"investment_nav_jobs", "investment_nav_watches", "investment_nav_series"} {
		var n int
		if err = target.DB.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, tenant).Scan(&n); err != nil || n != 0 {
			t.Fatal("restore created valuation work for a saved selection", table, n, err)
		}
	}
	// Exact changed edit query, representative synthetic saved-view cardinality.
	raw, _ := json.Marshal(saved)
	if _, err = pool.DB.Exec(ctx, `INSERT INTO investment_portfolios(id,tenant_id,actor_id,version,data) SELECT id,$1,$2,2,jsonb_set($3::jsonb,'{ID}',to_jsonb(id::text)) FROM (SELECT gen_random_uuid() AS id FROM generate_series(1,1000)) s`, tenant, actor, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, `ANALYZE investment_portfolios`); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.DB.Query(ctx, `EXPLAIN SELECT version,data FROM investment_portfolios WHERE tenant_id=$1 AND actor_id=$2 AND id=$3 FOR UPDATE`, tenant, actor, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, line)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	t.Log(strings.Join(plans, "\n"))
}
