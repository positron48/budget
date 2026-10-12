package postgres

import (
	"context"
	"github.com/positron48/budget/internal/adapter/tinvest"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"os"
	"os/user"
	"testing"
	"time"
)

// Explicit opt-in: public bond data only, credentials remain in Keychain/client.
func TestMarketBondLive(t *testing.T) {
	if os.Getenv("TINVEST_LIVE_TEST") != "1" {
		t.Skip("set TINVEST_LIVE_TEST=1 for public bond verification")
	}
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	account, err := user.Current()
	if err != nil {
		t.Fatal("macOS account unavailable")
	}
	client, err := tinvest.NewFromKeychain(ctx, account.Username)
	if err != nil {
		t.Fatal(err)
	}
	r := NewMarketDataRepo(pool)
	client.SetRequestGate(func(ctx context.Context, key string) error { return r.WaitMarketPermit(ctx, key, 600*time.Millisecond) })
	result, err := client.SearchMarketCatalog(ctx, md.JobRequest{Kind: "catalog_search", SearchQuery: "SU26238RMFS4", InstrumentKind: "bond"})
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, i := range result.Instruments {
		if i.Ticker == "SU26238RMFS4" && i.Kind == "bond" && i.ClassCode == "TQOB" {
			if id != "" {
				t.Fatal("ambiguous public listing")
			}
			id = i.ID
		}
	}
	if id == "" {
		t.Fatal("public bond not found")
	}
	var tenant string
	if err = pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Disposable public bond proof','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	s := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "disposable-proof"}
	if err = r.ConfigureSource(ctx, s, "Public bond proof", true); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	q := md.JobRequest{Kind: "bond_facts", InstrumentID: id, From: at, To: at.AddDate(0, 0, 30)}
	if _, err = r.Enqueue(ctx, s, q); err != nil {
		t.Fatal(err)
	}
	if err = (md.Worker{Queue: r, Provider: client, Scope: s, Lease: 2 * time.Minute}).RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := r.BondFacts(ctx, s, q)
	if err != nil || got == nil {
		t.Fatal("public bond snapshot not published", err)
	}
	if got.Instrument.Kind != "bond" || got.Terms.Nominal == nil || len(got.Accrued) == 0 {
		t.Fatal("missing public bond evidence")
	}
	t.Logf("Public bond snapshot persisted: coupons=%d events=%d accrued=%d", len(got.Coupons), len(got.Events), len(got.Accrued))
}
