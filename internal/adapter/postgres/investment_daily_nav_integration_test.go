package postgres

import (
	"context"
	"encoding/json"
	"errors"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"reflect"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentDailyNAVFencingAndGaps(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Daily NAV','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('daily-nav@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", day, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	deposit := inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "100", OccurredAt: day.Add(time.Hour)}}
	if _, e = svc.Mutate(ctx, tenant, actor, deposit); e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market"}
	first, e := repo.BuildDailyNAV(ctx, q, "2024-01-01")
	if e != nil || first.State != "ready" || first.Result.KnownValue.RatString() != "100" {
		t.Fatal(first, e)
	}
	again, e := repo.BuildDailyNAV(ctx, q, "2024-01-01")
	if e != nil || !again.ComputedAt.Equal(first.ComputedAt) {
		t.Fatal("idempotent rebuild changed point", again, e)
	}
	history, e := repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-02")
	if e != nil || len(history) != 2 || history[0].Result.KnownValue.RatString() != "100" || history[1].State != "unavailable" || history[1].Result != nil {
		t.Fatal(history, e)
	}

	summary, err := readDailyNAVHistoryMode(ctx, pool.DB, q, "2024-01-01", "2024-01-02", true)
	if err != nil || len(summary) != 2 || summary[0].Result.KnownValue.Cmp(history[0].Result.KnownValue) != 0 || summary[0].State != history[0].State || !summary[0].Result.At.Equal(history[0].Result.At) || summary[0].Result.Currency != history[0].Result.Currency || summary[0].Result.FXPolicy != history[0].Result.FXPolicy || !reflect.DeepEqual(summary[0].Result.Issues, history[0].Result.Issues) || !reflect.DeepEqual(summary[0].Result.Warnings, history[0].Result.Warnings) || summary[1].Result != nil || summary[1].State != "unavailable" || len(summary[0].Result.Components) != 0 || len(history[0].Result.Components) == 0 {
		t.Fatal("summary changed chart semantics", summary, err)
	}

	// A partial projection must keep its issue and nonblocking warning in the
	// public summary, never turn a known subtotal into a complete valuation.
	partialProjection := *first.Result
	partialProjection.Complete = false
	partialProjection.Issues = []calc.QualityIssue{{ComponentID: "stock", Code: "price_missing"}}
	partialProjection.Warnings = []calc.QualityIssue{{ComponentID: "stock", Code: "unknown_acquisition_cost"}}
	partialRaw, err := json.Marshal(partialProjection)
	if err != nil {
		t.Fatal(err)
	}
	originalRaw, err := json.Marshal(first.Result)
	if err != nil {
		t.Fatal(err)
	}
	seriesKey, _, err := navSeries(q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE investment_daily_nav SET data=$3 WHERE tenant_id=$1 AND series_key=$2`, tenant, seriesKey, partialRaw); err != nil {
		t.Fatal(err)
	}
	partialSummary, err := readDailyNAVHistoryMode(ctx, pool.DB, q, "2024-01-01", "2024-01-01", true)
	if err != nil || len(partialSummary) != 1 || partialSummary[0].State != "incomplete" || partialSummary[0].Result.Complete || !reflect.DeepEqual(partialSummary[0].Result.Issues, partialProjection.Issues) || !reflect.DeepEqual(partialSummary[0].Result.Warnings, partialProjection.Warnings) {
		t.Fatal("summary lost quality", partialSummary, err)
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE investment_daily_nav SET data=$3 WHERE tenant_id=$1 AND series_key=$2`, tenant, seriesKey, originalRaw); err != nil {
		t.Fatal(err)
	}
	// Read result, then change inputs before publication: the old generation must
	// lose the publication race, even if its calculation is internally consistent.
	q.At, _ = nav.DayCutoff("2024-01-01")
	stale, e := repo.ReadInvestmentValuation(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	deposit.RequestKey = "more"
	deposit.ExpectedLedgerVersion = 2
	deposit.Operation.Amount = "50"
	if _, e = svc.Mutate(ctx, tenant, actor, deposit); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.publishDailyNAV(ctx, q, "2024-01-01", stale); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale worker published", e)
	}
	history, e = repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-01")
	if e != nil || len(history) != 1 || history[0].State != "stale" || history[0].Result != nil {
		t.Fatal("old value exposed", history, e)
	}
	summary, err = readDailyNAVHistoryMode(ctx, pool.DB, q, "2024-01-01", "2024-01-01", true)
	if err != nil || len(summary) != 1 || summary[0].State != "stale" || summary[0].Result != nil {
		t.Fatal("summary exposed stale value", summary, err)
	}
	next, e := repo.BuildDailyNAV(ctx, q, "2024-01-01")
	if e != nil || next.State != "ready" || next.Result.KnownValue.RatString() != "150" || next.Generation <= first.Generation {
		t.Fatal(next, e)
	}
	var revisions int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_daily_nav_revisions WHERE tenant_id=$1`, tenant).Scan(&revisions); e != nil || revisions != 2 {
		t.Fatal(revisions, e)
	}
	// Same-generation divergent output cannot overwrite a recorded projection.
	tampered := *next.Result
	tampered.Warnings = append(tampered.Warnings, stale.Warnings...)
	if _, e = repo.publishDailyNAV(ctx, q, "2024-01-01", tampered); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("divergent same-generation value accepted", e)
	}
	// Publishing current data and its audit revision is atomic as well.
	deposit.RequestKey = "third"
	deposit.ExpectedLedgerVersion = 3
	deposit.Operation.Amount = "1"
	if _, e = svc.Mutate(ctx, tenant, actor, deposit); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION reject_nav_revision() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic NAV revision failure'; END $$; CREATE TRIGGER reject_nav_revision BEFORE INSERT ON investment_daily_nav_revisions FOR EACH ROW EXECUTE FUNCTION reject_nav_revision()`); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.BuildDailyNAV(ctx, q, "2024-01-01"); e == nil {
		t.Fatal("projection audit failure ignored")
	}
	history, e = repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-01")
	if e != nil || history[0].Result != nil || history[0].Generation != next.Generation {
		t.Fatal("partial publication", history, e)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER reject_nav_revision ON investment_daily_nav_revisions; DROP FUNCTION reject_nav_revision()`); e != nil {
		t.Fatal(e)
	}
	// Display currency/policy/convention definition is part of the series identity.
	q.Currency = "USD"
	partial, e := repo.BuildDailyNAV(ctx, q, "2024-01-01")
	if e != nil || partial.State != "incomplete" || partial.Result.Complete || partial.Result.KnownValue.Sign() != 0 {
		t.Fatal(partial, e)
	}
	history, e = repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-01")
	if e != nil || len(history) != 1 || history[0].State != "incomplete" {
		t.Fatal(history, e)
	}
	// Source entitlement changes also invalidate derivatives, even without quotes.
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "test"}
	if e = repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.BuildDailyNAV(ctx, q, "2024-01-01"); e != nil {
		t.Fatal(e)
	}
	if e = repo.ConfigureSource(ctx, scope, "Synthetic", false); e != nil {
		t.Fatal(e)
	}
	history, e = repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-01")
	if e != nil || history[0].Result != nil {
		t.Fatal("disabled-source derivative exposed", history, e)
	}
	// Empty membership returns no cached financial values.
	if _, e = pool.DB.Exec(ctx, `DELETE FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	history, e = repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-01")
	if e != nil || len(history) != 0 {
		t.Fatal("cache bypassed membership", history, e)
	}
	applyMigrations(t, ctx, pool.DB)
	assertInvestmentRollbackRefused(t, ctx, pool, "0027_investment_daily_nav")
}
