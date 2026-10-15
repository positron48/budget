package postgres

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestPortfolioQuoteMetadataBatchScopeAndActualPlan(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, _ := acceptanceOwner(t, pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	repo := NewMarketDataRepo(pool)
	if e := repo.ConfigureSource(ctx, scope, "Synthetic metadata source", true); e != nil {
		t.Fatal(e)
	}
	tx, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	mappings := map[string]md.Mapping{}
	refs := []map[string]string{}
	for n := 0; n < 1002; n++ {
		item := md.Instrument{ID: uuid.NewString(), Ticker: fmt.Sprintf("S%d", n), Name: "Synthetic metadata", ISIN: fmt.Sprintf("SYNTH%07d", n), Kind: "share", Currency: "RUB", ClassCode: "SYNTHETIC"}
		if _, e = saveMarketInstrument(ctx, tx, scope, item); e != nil {
			t.Fatal(e)
		}
		if n < 500 {
			key := item.Ticker
			mappings[key] = md.Mapping{Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: item.ID, From: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}}}
			refs = append(refs, map[string]string{"key": key, "provider": scope.Provider, "scope": scope.AccessScope, "id": item.ID})
		}
	}
	at := time.Now().UTC()
	got, times, e := readPortfolioQuoteMetadata(ctx, tx, tenant, mappings, at)
	if e != nil || len(got) != 500 || len(times) != 500 {
		t.Fatal(len(got), len(times), e)
	}
	for key, i := range got {
		if i.Ticker != key || i.ISIN == "" || times[key].After(at) {
			t.Fatal(key, i, times[key])
		}
	}
	raw, _ := json.Marshal(refs)
	if _, e = tx.Exec(ctx, `ANALYZE market_provider_instruments; ANALYZE market_data_sources`); e != nil {
		t.Fatal(e)
	}
	plan, e := tx.Query(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT TEXT) "+portfolioQuoteMetadataSQL, tenant, raw, at)
	if e != nil {
		t.Fatal(e)
	}
	for plan.Next() {
		var line string
		if e = plan.Scan(&line); e != nil {
			t.Fatal(e)
		}
		t.Log(line)
	}
	e = plan.Err()
	plan.Close()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE market_data_sources SET enabled=false WHERE tenant_id=$1`, tenant); e != nil {
		t.Fatal(e)
	}
	got, _, e = readPortfolioQuoteMetadata(ctx, tx, tenant, mappings, at)
	if e != nil || len(got) != 0 {
		t.Fatal("disabled metadata exposed", len(got), e)
	}
	if _, e = tx.Exec(ctx, `UPDATE market_data_sources SET enabled=true WHERE tenant_id=$1`, tenant); e != nil {
		t.Fatal(e)
	}
	got, _, e = readPortfolioQuoteMetadata(ctx, tx, uuid.NewString(), mappings, at)
	if e != nil || len(got) != 0 {
		t.Fatal("cross-tenant metadata exposed", len(got), e)
	}
	got, _, e = readPortfolioQuoteMetadata(ctx, tx, tenant, mappings, time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	if e != nil || len(got) != 0 {
		t.Fatal("future metadata exposed", len(got), e)
	}
}
