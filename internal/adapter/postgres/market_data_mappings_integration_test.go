package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestMarketMappingPersistence(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, foreign, actor string
	for _, v := range []*string{&tenant, &foreign} {
		if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Mappings','RUB') RETURNING id`).Scan(v); e != nil {
			t.Fatal(e)
		}
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('mapping@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	svc := md.NewMappingService(repo, NewTenantRepo(pool))
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := repo.ConfigureSource(ctx, scope, "Personal", true); e != nil {
		t.Fatal(e)
	}
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "SBER", Name: "Sber", ClassCode: "TQBR", Kind: "share", Currency: "RUB"}
	tx, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, scope, instrument); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	c := md.MappingChange{InstrumentKey: "SBER", RequestKey: "first", Reason: "Confirmed venue", Periods: []md.MappingPeriod{{Provider: "tinvest", AccessScope: "personal", InstrumentID: instrument.ID, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: at, Evidence: "Reviewed broker listing"}}}
	first, e := svc.Save(ctx, tenant, actor, c)
	if e != nil || first.Version != 1 {
		t.Fatal(first, e)
	}
	if again, e := svc.Save(ctx, tenant, actor, c); e != nil || again.Version != 1 || !again.RecordedAt.Equal(first.RecordedAt) {
		t.Fatal("retry changed evidence", again, e)
	}
	changed := c
	changed.Reason = "different"
	if _, e = svc.Save(ctx, tenant, actor, changed); !errors.Is(e, md.ErrConflict) {
		t.Fatal("request key reused", e)
	}
	if _, e = svc.Get(ctx, foreign, actor, "SBER"); !errors.Is(e, md.ErrForbidden) {
		t.Fatal("foreign access", e)
	}
	if _, e = repo.GetMapping(ctx, foreign, "SBER"); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("tenant leak", e)
	}
	if _, e = pool.DB.Exec(ctx, `UPDATE user_tenants SET role='member' WHERE tenant_id=$1 AND user_id=$2`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Save(ctx, tenant, actor, c); !errors.Is(e, md.ErrForbidden) {
		t.Fatal("member confirmed terms", e)
	}
	if _, e = svc.Get(ctx, tenant, actor, "SBER"); e != nil {
		t.Fatal("member read", e)
	}
	if _, e = pool.DB.Exec(ctx, `UPDATE user_tenants SET role='owner' WHERE tenant_id=$1 AND user_id=$2`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	// Two writers with the same expected version cannot overwrite each other.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			change := c
			change.ExpectedVersion = 1
			change.RequestKey = fmt.Sprintf("race-%d", i)
			_, err := svc.Save(ctx, tenant, actor, change)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, md.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal(wins, conflicts)
	}
	// Missing source and cross-scope instrument evidence cannot be confirmed.
	c.ExpectedVersion = 2
	c.RequestKey = "next"
	if e = repo.ConfigureSource(ctx, scope, "Personal", false); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Save(ctx, tenant, actor, c); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("disabled source", e)
	}
	if e = repo.ConfigureSource(ctx, scope, "Personal", true); e != nil {
		t.Fatal(e)
	}
	c.Periods[0].InstrumentID = uuid.NewString()
	if _, e = svc.Save(ctx, tenant, actor, c); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("unloaded listing", e)
	}
	c.Periods[0].InstrumentID = instrument.ID
	// Simulate invalidation failure; both current mapping and audit must roll back.
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION reject_mapping_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic invalidation failure'; END $$; CREATE TRIGGER reject_mapping_change BEFORE INSERT ON investment_market_mapping_changes FOR EACH ROW EXECUTE FUNCTION reject_mapping_change()`); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Save(ctx, tenant, actor, c); e == nil {
		t.Fatal("feed failure ignored")
	}
	current, e := svc.Get(ctx, tenant, actor, "SBER")
	if e != nil || current.Version != 2 {
		t.Fatal(current, e)
	}
	var count int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_market_mapping_revisions WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 2 {
		t.Fatal(count, e)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER reject_mapping_change ON investment_market_mapping_changes; DROP FUNCTION reject_mapping_change()`); e != nil {
		t.Fatal(e)
	}
	// Clearing a mistaken mapping retains its original affected date and history.
	c.Periods = nil
	cleared, e := svc.Save(ctx, tenant, actor, c)
	if e != nil || cleared.Version != 3 || len(cleared.Periods) != 0 {
		t.Fatal(cleared, e)
	}
	var affected time.Time
	if e = pool.DB.QueryRow(ctx, `SELECT affected_from FROM investment_market_mapping_changes WHERE tenant_id=$1 AND version=3`, tenant).Scan(&affected); e != nil || !affected.Equal(at) {
		t.Fatal(affected, e)
	}
	if retry, e := svc.Save(ctx, tenant, actor, md.MappingChange{InstrumentKey: first.InstrumentKey, RequestKey: "first", Reason: first.Reason, Periods: first.Periods}); e != nil || retry.Version != 1 {
		t.Fatal("old retry returned current revision", retry, e)
	}
	estimates := []md.PriceEstimate{{Price: "0", Currency: "RUB", At: at, From: at, Reference: "Explicit owner estimate"}}
	estimated, e := svc.Save(ctx, tenant, actor, md.MappingChange{InstrumentKey: "SBER", ExpectedVersion: 3, RequestKey: "estimate", Reason: "Dated manual valuation", PriceEstimates: &estimates})
	if e != nil || len(estimated.PriceEstimates) != 1 {
		t.Fatal(estimated, e)
	}
	quotes, e := repo.InvestmentQuotes(ctx, tenant, actor, []string{"SBER"})
	if e != nil || len(quotes.Quotes) != 1 || quotes.Quotes[0].State != "estimated" || quotes.Quotes[0].CleanPrice.Sign() != 0 || !quotes.Quotes[0].At.Equal(at) {
		t.Fatal("explicit zero estimate unavailable", quotes, e)
	}
	preserved, e := svc.Save(ctx, tenant, actor, md.MappingChange{InstrumentKey: "SBER", ExpectedVersion: 4, RequestKey: "old-client", Reason: "Mapping-only update"})
	if e != nil || len(preserved.PriceEstimates) != 1 {
		t.Fatal("legacy client erased estimate", preserved, e)
	}
	empty := []md.PriceEstimate{}
	if _, e = svc.Save(ctx, tenant, actor, md.MappingChange{InstrumentKey: "SBER", ExpectedVersion: 5, RequestKey: "clear-estimate", Reason: "Remove manual estimate", PriceEstimates: &empty}); e != nil {
		t.Fatal(e)
	}
	quotes, e = repo.InvestmentQuotes(ctx, tenant, actor, []string{"SBER"})
	if e != nil || quotes.Quotes[0].CleanPrice != nil {
		t.Fatal("removed estimate still used", quotes, e)
	}
	applyMigrations(t, ctx, pool.DB)
	assertInvestmentRollbackRefused(t, ctx, pool, "0025_investment_market_mappings")
}
