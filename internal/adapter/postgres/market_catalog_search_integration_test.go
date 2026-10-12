package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestMarketCatalogSearchJobs(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant string
	if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Search','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if err := repo.ConfigureSource(ctx, scope, "Source", true); err != nil {
		t.Fatal(err)
	}
	request := md.JobRequest{Kind: "catalog_search", SearchQuery: "  SaMe ", InstrumentKind: "share"}
	first, err := repo.RequestRefresh(ctx, scope, request)
	if err != nil || first.InstrumentID != "" || first.SearchQuery != "same" {
		t.Fatal(first, err)
	}
	request.SearchQuery = "same"
	again, err := repo.RequestRefresh(ctx, scope, request)
	if err != nil || again.ID != first.ID {
		t.Fatal("query dedup", again, err)
	}
	job, err := repo.Claim(ctx, scope, time.Minute)
	if err != nil || job.SearchQuery != "same" || job.InstrumentKind != "share" {
		t.Fatal(job, err)
	}
	if err = repo.Complete(ctx, job, nil); !errors.Is(err, md.ErrInvalid) {
		t.Fatal("catalog job completed as price request", err)
	}
	item := md.Instrument{ID: uuid.NewString(), Ticker: "SAME", Name: "Same Company", Kind: "share", ClassCode: "EX1"}
	other := item
	other.ID = uuid.NewString()
	other.ClassCode = "EX2"
	invalid := other
	invalid.Kind = "bond"
	if err = repo.CompleteCatalog(ctx, job, md.CatalogResult{Instruments: []md.Instrument{item, invalid}}); !errors.Is(err, md.ErrInvalid) {
		t.Fatal(err)
	}
	found, err := repo.SearchCatalog(ctx, scope, "same", 50, 0)
	if err != nil || len(found) != 0 {
		t.Fatal("partial catalog batch published", found, err)
	}
	if err = repo.CompleteCatalog(ctx, job, md.CatalogResult{Instruments: []md.Instrument{item, other}, Truncated: true}); err != nil {
		t.Fatal(err)
	}
	status, err := repo.JobStatus(ctx, tenant, job.ID)
	if err != nil || status.Status != "ready" || status.CoverageState != "catalog_truncated" || status.InstrumentCount != 2 || status.ObservationCount != 0 {
		t.Fatal(status, err)
	}
	if err = repo.CompleteCatalog(ctx, job, md.CatalogResult{}); !errors.Is(err, md.ErrLeaseLost) {
		t.Fatal(err)
	}
	// Short search metadata must not erase the currency of a detailed load.
	_, err = repo.Enqueue(ctx, scope, md.JobRequest{Kind: "last_price", InstrumentID: item.ID})
	if err != nil {
		t.Fatal(err)
	}
	quoteJob, err := repo.Claim(ctx, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	detailed := item
	detailed.Currency = "RUB"
	detailed.Detailed = true
	if err = repo.CompleteWithInstrument(ctx, quoteJob, detailed, nil); err != nil {
		t.Fatal(err)
	}
	_, err = repo.Enqueue(ctx, scope, request)
	if err != nil {
		t.Fatal(err)
	}
	searchJob, err := repo.Claim(ctx, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.CompleteCatalog(ctx, searchJob, md.CatalogResult{Instruments: []md.Instrument{item}}); err != nil {
		t.Fatal(err)
	}
	found, err = repo.SearchCatalog(ctx, scope, item.ID, 50, 0)
	if err != nil || len(found) != 1 || found[0].Currency != "RUB" || !found[0].Detailed || found[0].Revision != 2 {
		t.Fatal("summary downgraded metadata", found, err)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0024_market_catalog_search")
	applyMigrations(t, ctx, pool.DB)
}
