package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestInvestmentPriceConventions(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Convention NAV','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('conventions@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", day, "create")
	if e != nil {
		t.Fatal(e)
	}
	deposit := inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "1000", OccurredAt: day.Add(time.Hour)}}
	if _, e = svc.Mutate(ctx, tenant, actor, deposit); e != nil {
		t.Fatal(e)
	}
	buy := inv.Mutation{Action: "create", RequestKey: "buy", ExpectedLedgerVersion: 2, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "S", Currency: "RUB", Amount: "100", Quantity: "1", OccurredAt: day.Add(2 * time.Hour)}}
	if _, e = svc.Mutate(ctx, tenant, actor, buy); e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e = repo.ConfigureSource(ctx, scope, "Test", true); e != nil {
		t.Fatal(e)
	}
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "S", Name: "Synthetic share", ClassCode: "TEST", Kind: "share", Currency: "RUB"}
	setup, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, setup, scope, instrument); e != nil {
		t.Fatal(e)
	}
	if e = setup.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "mapping", Reason: "Synthetic verified terms", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: day, Evidence: "Test evidence"}}}); e != nil {
		t.Fatal(e)
	}
	price := md.Observation{InstrumentID: instrument.ID, Kind: "last_price", At: day.Add(4 * time.Hour), SourceKind: "exchange", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Currency: "RUB", Price: "120", Complete: true}
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{price}); e != nil {
		t.Fatal(e)
	}
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, At: day.Add(6 * time.Hour), Currency: "RUB", FXPolicyKey: "market-test", Series: map[string]nav.SeriesPolicy{"S": {Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", MaxQuoteAge: 24 * time.Hour}}}
	profile := nav.ConventionProfile{Key: "reviewed", Title: "Verified calendar and price rules", Evidence: "Synthetic source documentation", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{"S": {{Scope: scope, InstrumentID: instrument.ID, From: day, Policy: q.Series["S"]}}}}
	saved, e := repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0)
	if e != nil || saved.Version != 1 {
		t.Fatal(saved, e)
	}
	q.Series = nil
	q.ConventionKey = profile.Key
	got, e := repo.ReadInvestmentValuation(ctx, q)
	if e != nil || !got.Complete || got.KnownValue.RatString() != "1020" || got.ConventionVersion != 1 {
		t.Fatal(got, e)
	}
	if _, e = repo.BuildDailyNAV(ctx, q, "2024-01-01"); e != nil {
		t.Fatal(e)
	}
	frozen, e := pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = frozen.Rollback(ctx) }()
	var anchor int64
	if e = frozen.QueryRow(ctx, `SELECT version FROM investment_price_conventions WHERE tenant_id=$1`, tenant).Scan(&anchor); e != nil {
		t.Fatal(e)
	}
	changed := profile
	changed.Periods = map[string][]nav.PriceConventionPeriod{"S": {profile.Periods["S"][0]}}
	changed.Periods["S"][0].Policy.MaxQuoteAge = time.Hour
	updated, e := repo.RegisterPriceConventions(ctx, tenant, actor, changed, 1)
	if e != nil || updated.Version != 2 {
		t.Fatal(updated, e)
	}
	got, e = readInvestmentValuation(ctx, frozen, q)
	if e != nil || !got.Complete || got.ConventionVersion != 1 || got.KnownValue.RatString() != "1020" {
		t.Fatal("MVCC mixed convention", got, e)
	}
	if e = frozen.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	got, e = repo.ReadInvestmentValuation(ctx, q)
	if e != nil || got.Complete || got.ConventionVersion != 2 || got.KnownValue.RatString() != "900" {
		t.Fatal("new age ignored", got, e)
	}
	history, e := repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-01")
	if e != nil || history[0].State != "stale" || history[0].Result != nil {
		t.Fatal("revision did not invalidate", history, e)
	}
	retry, e := repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0)
	if e != nil || retry.Version != 1 {
		t.Fatal("original retry", retry, e)
	}
	profile.Title = "Conflicting edit"
	if _, e = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal(e)
	}
	// Audit failure must roll back both version and source generation.
	var before, after int64
	if e = pool.DB.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION reject_convention_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit failure'; END $$; CREATE TRIGGER reject_convention_audit BEFORE INSERT ON investment_price_convention_revisions FOR EACH ROW EXECUTE FUNCTION reject_convention_audit()`); e != nil {
		t.Fatal(e)
	}
	changed.Enabled = false
	if _, e = repo.RegisterPriceConventions(ctx, tenant, actor, changed, 2); e == nil {
		t.Fatal("audit failure ignored")
	}
	if e = pool.DB.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&after); e != nil || before != after {
		t.Fatal(before, after, e)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER reject_convention_audit ON investment_price_convention_revisions`); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.RegisterPriceConventions(ctx, tenant, actor, changed, 2); e != nil {
		t.Fatal(e)
	}
	got, e = repo.ReadInvestmentValuation(ctx, q)
	if e != nil || got.Complete || got.ConventionVersion != 3 {
		t.Fatal("revoked profile used", got, e)
	}

	other := instrument
	other.ID = uuid.NewString()
	tx, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, scope, other); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	changed.Enabled = true
	changed.Periods["S"][0].InstrumentID = other.ID
	changed.Periods["S"][0].Policy.MaxQuoteAge = 24 * time.Hour
	if _, e = repo.RegisterPriceConventions(ctx, tenant, actor, changed, 3); e != nil {
		t.Fatal(e)
	}
	got, e = repo.ReadInvestmentValuation(ctx, q)
	if e != nil || got.Complete {
		t.Fatal("cross-listing convention accepted", got, e)
	}
	found := false
	for _, issue := range got.Issues {
		if issue.Code == "price_policy_scope_mismatch" {
			found = true
		}
	}
	if !found {
		t.Fatal("listing mismatch not explained", got.Issues)
	}
	assertInvestmentCurrentDailyPolicy(t, ctx, repo, q, scope, instrument, day)
	assertInvestmentReviewedAccrued(t, ctx, repo, q, scope, instrument, day)
	if _, e = pool.DB.Exec(ctx, `UPDATE user_tenants SET role='member' WHERE tenant_id=$1 AND user_id=$2`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.RegisterPriceConventions(ctx, tenant, actor, changed, 4); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("member approved conventions", e)
	}
	q.Series = map[string]nav.SeriesPolicy{"S": {}}
	if _, e = repo.ReadInvestmentValuation(ctx, q); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal("inline policy override accepted", e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0032_investment_price_conventions")
}
