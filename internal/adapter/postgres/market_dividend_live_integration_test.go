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

// Explicit opt-in: public dividend data only, credentials remain in Keychain/client.
func TestMarketDividendLive(t *testing.T) {
	if os.Getenv("TINVEST_LIVE_TEST") != "1" {
		t.Skip("set TINVEST_LIVE_TEST=1 for public dividend verification")
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
	result, err := client.SearchMarketCatalog(ctx, md.JobRequest{Kind: "catalog_search", SearchQuery: "SBER", InstrumentKind: "share"})
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, i := range result.Instruments {
		if i.Ticker == "SBER" && i.Kind == "share" && i.ClassCode == "TQBR" {
			if id != "" {
				t.Fatal("ambiguous public listing")
			}
			id = i.ID
		}
	}
	if id == "" {
		t.Fatal("public dividend not found")
	}
	var tenant string
	if err = pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Disposable public dividend proof','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	s := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "disposable-proof"}
	if err = r.ConfigureSource(ctx, s, "Public dividend proof", true); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	q := md.JobRequest{Kind: "dividend_facts", InstrumentID: id, From: at, To: at.AddDate(0, 0, 364)}
	if _, err = r.Enqueue(ctx, s, q); err != nil {
		t.Fatal(err)
	}
	if err = (md.Worker{Queue: r, Provider: client, Scope: s, Lease: 2 * time.Minute}).RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := r.DividendFacts(ctx, s, q)
	if err != nil || got == nil {
		t.Fatal("public dividend snapshot not published", err)
	}
	if got.Instrument.Kind != "share" || len(got.Dividends) == 0 {
		t.Fatal("missing public declaration evidence")
	}
	t.Logf("Public dividend snapshot persisted: declarations=%d", len(got.Dividends))
}
