package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestMarketObservationPersistence(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, foreign string
	for _, id := range []*string{&tenant, &foreign} {
		if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Market','RUB') RETURNING id`).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	observation := md.Observation{InstrumentID: "11111111-1111-4111-8111-111111111111", Kind: "daily_candle", At: at, SourceKind: "exchange", PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Price: "9007199254740993.000000001", Open: "9007199254740993", High: "9007199254740994", Low: "9007199254740992", Volume: "1", Complete: true}
	saved, err := repo.SaveObservations(ctx, scope, []md.Observation{observation})
	if err != nil || saved[0].Revision != 1 {
		t.Fatal(saved, err)
	}
	id := saved[0].ID
	// Economically equal decimals are idempotent, even under concurrent retries.
	observation.Open = "9007199254740993.000000000"
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := repo.SaveObservations(ctx, scope, []md.Observation{observation})
			if err == nil && (got[0].Revision != 1 || got[0].ID != id) {
				err = errors.New("retry changed revision")
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	observation.Price = "9007199254740993.000000002"
	saved, err = repo.SaveObservations(ctx, scope, []md.Observation{observation})
	if err != nil || saved[0].Revision != 2 {
		t.Fatal(saved, err)
	}
	var revisions, changes int
	var original string
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_provider_observation_revisions WHERE tenant_id=$1 AND observation_id=$2`, tenant, id).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_changes WHERE tenant_id=$1 AND observation_id=$2`, tenant, id).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT data->>'price' FROM market_provider_observation_revisions WHERE tenant_id=$1 AND observation_id=$2 AND revision=1`, tenant, id).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if revisions != 2 || changes != 2 || original != "9007199254740993.000000001" {
		t.Fatal(revisions, changes, original)
	}
	read, err := repo.Observations(ctx, scope, observation.InstrumentID, observation.Kind, at, at)
	if err != nil || len(read) != 1 || read[0].Price != observation.Price || read[0].Revision != 2 {
		t.Fatal(read, err)
	}
	other := scope
	other.TenantID = foreign
	read, err = repo.Observations(ctx, other, observation.InstrumentID, observation.Kind, at, at)
	if err != nil || len(read) != 0 {
		t.Fatal("tenant leak", read, err)
	}
	other = scope
	other.AccessScope = "separate-license"
	read, err = repo.Observations(ctx, other, observation.InstrumentID, observation.Kind, at, at)
	if err != nil || len(read) != 0 {
		t.Fatal("entitlement leak", read, err)
	}
	if _, err = repo.SaveObservations(ctx, other, []md.Observation{observation}); err != nil {
		t.Fatal(err)
	}
	// A malformed later item cannot publish the first item in a batch.
	changed := observation
	changed.Price = "9007199254740993.000000003"
	bad := observation
	bad.At = bad.At.AddDate(0, 0, 1)
	bad.High = "1"
	if _, err = repo.SaveObservations(ctx, scope, []md.Observation{changed, bad}); !errors.Is(err, md.ErrInvalid) {
		t.Fatal(err)
	}
	read, err = repo.Observations(ctx, scope, observation.InstrumentID, observation.Kind, at, at)
	if err != nil || read[0].Revision != 2 {
		t.Fatal("partial invalid batch", read, err)
	}
	// Force a failure after the current row changed, proving SQL audit/feed atomicity.
	if _, err = pool.DB.Exec(ctx, `CREATE FUNCTION reject_market_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic feed failure'; END $$; CREATE TRIGGER reject_market_change BEFORE INSERT ON market_data_changes FOR EACH ROW EXECUTE FUNCTION reject_market_change()`); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveObservations(ctx, scope, []md.Observation{changed}); err == nil {
		t.Fatal("expected feed failure")
	}
	read, err = repo.Observations(ctx, scope, observation.InstrumentID, observation.Kind, at, at)
	if err != nil || read[0].Revision != 2 {
		t.Fatal("feed failure published a price", read, err)
	}
	if _, err = pool.DB.Exec(ctx, `DROP TRIGGER reject_market_change ON market_data_changes; DROP FUNCTION reject_market_change()`); err != nil {
		t.Fatal(err)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0019_market_observations")
	applyMigrations(t, ctx, pool.DB)
}
