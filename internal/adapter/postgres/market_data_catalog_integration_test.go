package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestMarketCatalogAtomicityAndIdentity(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, foreign string
	for _, id := range []*string{&tenant, &foreign} {
		if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Catalog','RUB') RETURNING id`).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	one := md.Instrument{ID: uuid.NewString(), Ticker: "SAME", ClassCode: "ONE", ISIN: "TEST-ISIN", Name: "First listing", Kind: "share", Currency: "USD"}
	two := one
	two.ID = uuid.NewString()
	two.ClassCode = "TWO"
	post := func(i md.Instrument) error {
		_, err := repo.Enqueue(ctx, scope, md.JobRequest{InstrumentID: i.ID, Kind: "last_price"})
		if err != nil {
			return err
		}
		job, err := repo.Claim(ctx, scope, time.Minute)
		if err != nil {
			return err
		}
		return repo.CompleteWithInstrument(ctx, job, i, nil)
	}
	if err := post(one); err != nil {
		t.Fatal(err)
	}
	if err := post(two); err != nil {
		t.Fatal(err)
	}
	found, err := repo.SearchCatalog(ctx, scope, "same", 50, 0)
	if err != nil || len(found) != 2 || found[0].ClassCode != "ONE" || found[1].ClassCode != "TWO" {
		t.Fatal("listings collapsed", found, err)
	}
	if err = post(one); err != nil {
		t.Fatal(err)
	}
	found, err = repo.SearchCatalog(ctx, scope, one.ID, 50, 0)
	if err != nil || len(found) != 1 || found[0].Revision != 1 {
		t.Fatal("identical metadata changed revision", found, err)
	}
	one.Name = "Corrected listing"
	if err = post(one); err != nil {
		t.Fatal(err)
	}
	found, err = repo.SearchCatalog(ctx, scope, "Corrected", 50, 0)
	if err != nil || len(found) != 1 || found[0].Revision != 2 {
		t.Fatal(found, err)
	}
	var oldName string
	if err = pool.DB.QueryRow(ctx, `SELECT data->>'name' FROM market_provider_instrument_revisions WHERE tenant_id=$1 AND instrument_id=$2 AND revision=1`, tenant, one.ID).Scan(&oldName); err != nil || oldName != "First listing" {
		t.Fatal(oldName, err)
	}
	for _, query := range []string{"%", "_", "' OR true --"} {
		found, err = repo.SearchCatalog(ctx, scope, query, 50, 0)
		if err != nil || len(found) != 0 {
			t.Fatal("literal search broadened", query, found, err)
		}
	}
	other := scope
	other.TenantID = foreign
	found, err = repo.SearchCatalog(ctx, other, "", 50, 0)
	if err != nil || len(found) != 0 {
		t.Fatal("tenant leak", found, err)
	}
	other = scope
	other.AccessScope = "another"
	found, err = repo.SearchCatalog(ctx, other, "", 50, 0)
	if err != nil || len(found) != 0 {
		t.Fatal("entitlement leak", found, err)
	}
	_, err = repo.Enqueue(ctx, scope, md.JobRequest{InstrumentID: one.ID, Kind: "last_price"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := repo.Claim(ctx, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.CompleteWithInstrument(ctx, job, two, nil); !errors.Is(err, md.ErrInvalid) {
		t.Fatal("foreign instrument published", err)
	}
	changed := one
	changed.Name = "Must roll back"
	invalid := md.Observation{InstrumentID: one.ID, Kind: "last_price", SourceKind: "exchange", Price: "not decimal"}
	if err = repo.CompleteWithInstrument(ctx, job, changed, []md.Observation{invalid}); !errors.Is(err, md.ErrInvalid) {
		t.Fatal(err)
	}
	found, err = repo.SearchCatalog(ctx, scope, one.ID, 50, 0)
	if err != nil || found[0].Name != one.Name || found[0].Revision != 2 {
		t.Fatal("partial metadata publication", found, err)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0023_market_catalog")
}
