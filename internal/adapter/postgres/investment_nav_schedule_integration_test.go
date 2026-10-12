package postgres

import (
	"context"
	"encoding/json"
	"errors"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"math/big"
	"testing"
	"time"
)

func TestInvestmentNAVSchedule(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('NAV scheduler','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('nav-schedule@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", day, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market"}
	if _, e = repo.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-03"); e != nil {
		t.Fatal(e)
	}
	if n, e := repo.ScheduleDailyNAV(ctx, tenant); e != nil || n != 3 {
		t.Fatal(n, e)
	}
	for i := 0; i < 3; i++ {
		if worked, e := repo.RunDailyNAVOnce(ctx, tenant); e != nil || !worked {
			t.Fatal(worked, e)
		}
	}
	if n, e := repo.ScheduleDailyNAV(ctx, tenant); e != nil || n != 0 {
		t.Fatal("unchanged rescheduled", n, e)
	}
	mutation := inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "100", OccurredAt: day.Add(25 * time.Hour)}}
	if _, e = svc.Mutate(ctx, tenant, actor, mutation); e != nil {
		t.Fatal(e)
	}
	if n, e := repo.ScheduleDailyNAV(ctx, tenant); e != nil || n != 2 {
		t.Fatal("date scope", n, e)
	}
	running, e := repo.claimDailyNAV(ctx, tenant)
	if e != nil || running.ID == "" {
		t.Fatal(running, e)
	}
	mutation.RequestKey = "second"
	mutation.ExpectedLedgerVersion = 2
	if _, e = svc.Mutate(ctx, tenant, actor, mutation); e != nil {
		t.Fatal(e)
	}
	// Force enqueue failure: neither cursor nor partial jobs may survive.
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION fail_nav_schedule() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic scheduling failure'; END $$; CREATE TRIGGER fail_nav_schedule BEFORE INSERT ON investment_nav_jobs FOR EACH ROW EXECUTE FUNCTION fail_nav_schedule()`); e != nil {
		t.Fatal(e)
	}
	var before, after int64
	if e = pool.DB.QueryRow(ctx, `SELECT cursor_generation FROM investment_nav_watches WHERE tenant_id=$1`, tenant).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.ScheduleDailyNAV(ctx, tenant); e == nil {
		t.Fatal("failure ignored")
	}
	if e = pool.DB.QueryRow(ctx, `SELECT cursor_generation FROM investment_nav_watches WHERE tenant_id=$1`, tenant).Scan(&after); e != nil || before != after {
		t.Fatal("cursor lost", before, after, e)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER fail_nav_schedule ON investment_nav_jobs`); e != nil {
		t.Fatal(e)
	}
	if n, e := repo.ScheduleDailyNAV(ctx, tenant); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	if e = repo.finishDailyNAV(ctx, running, nil); e != nil {
		t.Fatal(e)
	}
	var status string
	var attempts int
	if e = pool.DB.QueryRow(ctx, `SELECT status,attempts FROM investment_nav_jobs WHERE id=$1`, running.ID).Scan(&status, &attempts); e != nil || status != "pending" || attempts != 0 {
		t.Fatal("running invalidation lost", status, attempts, e)
	}
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_nav_jobs SET available_at=clock_timestamp() WHERE tenant_id=$1`, tenant); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if worked, e := repo.RunDailyNAVOnce(ctx, tenant); e != nil || !worked {
			t.Fatal(worked, e)
		}
	}
	history, e := repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-03")
	if e != nil || len(history) != 3 {
		t.Fatal(history, e)
	}
	for i, p := range history {
		want := "200"
		if i == 0 {
			want = "0"
		}
		if p.State != "ready" || p.Result.KnownValue.RatString() != want {
			t.Fatal(i, p)
		}
	}

	series, e := repo.ListNAVSeries(ctx, tenant, actor, "")
	if e != nil || len(series) != 1 || series[0].Currency != "RUB" || len(series[0].LedgerIDs) != 1 {
		t.Fatal(series, e)
	}
	key := series[0].Key
	if next, e := repo.ListNAVSeries(ctx, tenant, actor, key); e != nil || len(next) != 0 {
		t.Fatal(next, e)
	}
	publicHistory, e := repo.NAVHistoryForSeries(ctx, tenant, actor, key, "2024-01-01", "2024-01-03")
	if e != nil || len(publicHistory) != 3 || publicHistory[2].Result.KnownValue.RatString() != "200" {
		t.Fatal(publicHistory, e)
	}
	if _, e = repo.NAVHistoryForSeries(ctx, tenant, actor, key, "2023-12-31", "2024-01-03"); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal("range expansion allowed", e)
	}
	foreign := "11111111-1111-4111-8111-111111111111"
	if _, e = repo.NAVHistoryForSeries(ctx, foreign, actor, key, "2024-01-01", "2024-01-03"); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign tenant exposed", e)
	}
	if _, e = repo.NAVHistoryForSeries(ctx, tenant, foreign, key, "2024-01-01", "2024-01-03"); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign actor exposed", e)
	}
	if e = repo.RequestNAVRefresh(ctx, tenant, actor, key); e != nil {
		t.Fatal(e)
	}
	if e = repo.RequestNAVRefresh(ctx, tenant, actor, key); !errors.Is(e, d.ErrInvestmentLimit) {
		t.Fatal("refresh cooldown ignored", e)
	}
	if n, e := repo.ScheduleDailyNAV(ctx, tenant); e != nil || n != 3 {
		t.Fatal(n, e)
	}
	for i := 0; i < 3; i++ {
		if worked, e := repo.RunDailyNAVOnce(ctx, tenant); e != nil || !worked {
			t.Fatal(worked, e)
		}
	}
	mutation.RequestKey = "third"
	mutation.ExpectedLedgerVersion = 3
	if _, e = svc.Mutate(ctx, tenant, actor, mutation); e != nil {
		t.Fatal(e)
	}
	type scheduled struct {
		n   int
		err error
	}
	results := make(chan scheduled, 2)
	for i := 0; i < 2; i++ {
		go func() { n, err := repo.ScheduleDailyNAV(ctx, tenant); results <- scheduled{n, err} }()
	}
	total := 0
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		total += result.n
	}
	if total != 2 {
		t.Fatal("concurrent schedulers duplicated work", total)
	}
	last, e := repo.claimDailyNAV(ctx, tenant)
	if e != nil || last.ID == "" {
		t.Fatal(last, e)
	}
	// New input on a crashed final attempt must get a fresh retry budget.
	mutation.RequestKey = "fourth"
	mutation.ExpectedLedgerVersion = 4
	if _, e = svc.Mutate(ctx, tenant, actor, mutation); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.ScheduleDailyNAV(ctx, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_nav_jobs SET attempts=5,lease_until=clock_timestamp()-interval '1 second',available_at=clock_timestamp()-interval '1 day' WHERE id=$1`, last.ID); e != nil {
		t.Fatal(e)
	}
	reclaimed, e := repo.claimDailyNAV(ctx, tenant)
	if e != nil || reclaimed.ID != last.ID || reclaimed.Token == last.Token || reclaimed.Generation <= last.Generation {
		t.Fatal("new input lost after final crash", reclaimed, e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0030_investment_nav_schedule")
}

func TestInvestmentCompositionHistoryReadAndReclassification(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Composition','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('composition@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", at, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "100", OccurredAt: at}}); e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market"}
	if _, e = repo.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-02"); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if worked, e := repo.RunDailyNAVOnce(ctx, tenant); e != nil || !worked {
			t.Fatal(worked, e)
		}
	}
	key, _, e := navSeries(q)
	if e != nil {
		t.Fatal(e)
	}
	out, e := repo.InvestmentCompositionHistory(ctx, tenant, actor, key, "2024-01-01", "2024-01-02", nil, "group")
	if e != nil || len(out.Days) != 2 || out.Days[1].Composition == nil || out.Days[1].Composition.CashKnown.RatString() != "100" || !out.Days[1].Composition.Complete {
		t.Fatal(out, e)
	}
	if _, e = repo.InvestmentCompositionHistory(ctx, tenant, actor, key, "2023-12-31", "2024-01-02", nil, "group"); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal(e)
	}
	if _, e = repo.InvestmentCompositionHistory(ctx, tenant, "11111111-1111-4111-8111-111111111111", key, "2024-01-01", "2024-01-02", nil, "group"); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal(e)
	}
	// Synthetic projection components isolate read-time reclassification from the
	// market provider; the same cached values must be regrouped without a NAV job.
	groupID := "33333333-3333-4333-8333-333333333333"
	c := nav.Result{Components: map[string]nav.ComponentIdentity{"security": {LedgerID: ledger.ID, Kind: "position", Key: "ABC"}, "cash": {LedgerID: ledger.ID, Kind: "cash", Key: "RUB"}}}
	c.Complete = true
	c.KnownValue = big.NewRat(100, 1)
	c.ComponentValues = map[string]*big.Rat{"security": big.NewRat(80, 1), "cash": big.NewRat(20, 1)}
	raw, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_daily_nav SET data=$3 WHERE tenant_id=$1 AND series_key=$2`, tenant, key, raw); e != nil {
		t.Fatal(e)
	}
	classification, e := repo.SaveInstrumentGroups(ctx, tenant, actor, nav.SaveInstrumentGroups{InstrumentGroups: nav.InstrumentGroups{Groups: []nav.InstrumentGroup{{ID: groupID, Name: "Strategy"}}, Memberships: []nav.GroupMembership{{LedgerID: ledger.ID, InstrumentKey: "ABC", GroupID: groupID}}}, RequestKey: "groups", Reason: "Review"})
	if e != nil {
		t.Fatal(e)
	}
	out, e = repo.InvestmentCompositionHistory(ctx, tenant, actor, key, "2024-01-01", "2024-01-02", []string{groupID, groupID}, "group")
	if e != nil || out.Classification.Version != classification.Version || out.Days[0].Composition.SecuritiesKnown.RatString() != "80" || out.Days[0].Composition.CashKnown.RatString() != "20" {
		t.Fatal(out, e)
	}
	if _, e = repo.SaveInstrumentGroups(ctx, tenant, actor, nav.SaveInstrumentGroups{InstrumentGroups: nav.InstrumentGroups{Version: classification.Version}, RequestKey: "remove", Reason: "Remove strategy"}); e != nil {
		t.Fatal(e)
	}
	out, e = repo.InvestmentCompositionHistory(ctx, tenant, actor, key, "2024-01-01", "2024-01-02", []string{""}, "group")
	if e != nil || out.Days[0].Composition.SecuritiesKnown.RatString() != "80" {
		t.Fatal(out, e)
	}
	var pending int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_nav_jobs WHERE tenant_id=$1 AND status<>'ready'`, tenant).Scan(&pending); e != nil || pending != 0 {
		t.Fatal("classification enqueued NAV", pending, e)
	}
	if _, e = repo.InvestmentCompositionHistory(ctx, tenant, actor, key, "2024-01-01", "2024-01-02", []string{groupID}, "group"); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal(e)
	}
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_daily_nav SET data=data-'Components'-'ComponentValues' WHERE tenant_id=$1`, tenant); e != nil {
		t.Fatal(e)
	}
	out, e = repo.InvestmentCompositionHistory(ctx, tenant, actor, key, "2024-01-01", "2024-01-02", nil, "group")
	if e != nil || out.Days[0].Composition.SecuritiesKnown != nil || out.Days[0].Composition.Complete {
		t.Fatal("legacy cache treated as zero", out, e)
	}
}
