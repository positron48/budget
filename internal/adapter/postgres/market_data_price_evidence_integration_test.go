package postgres

import (
	"context"
	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestMarketPriceEvidenceRevisionCutoff(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Price evidence','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := repo.ConfigureSource(ctx, scope, "Personal", true); e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	o := md.Observation{InstrumentID: uuid.NewString(), Kind: "last_price", At: at, SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "currency_per_unit", Currency: "RUB", Price: "100", Complete: true}
	first, e := repo.SaveObservations(ctx, scope, []md.Observation{o})
	if e != nil {
		t.Fatal(e)
	}
	o.Price = "101"
	second, e := repo.SaveObservations(ctx, scope, []md.Observation{o})
	if e != nil {
		t.Fatal(e)
	}
	if !second[0].ReceivedAt.After(first[0].ReceivedAt) {
		t.Fatal("revision timestamps not ordered")
	}
	// A dealer quote at the same time belongs to another series.
	dealer := o
	dealer.SourceKind = "dealer"
	dealer.Price = "999"
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{dealer}); e != nil {
		t.Fatal(e)
	}
	q := md.PriceSelectionRequest{Scope: scope, InstrumentID: o.InstrumentID, Kind: o.Kind, SourceKind: o.SourceKind, Adjustment: o.Adjustment, At: at.Add(time.Minute), MaxQuoteAge: time.Minute}
	check := func(want string, revision int64) {
		t.Helper()
		values, e := repo.PriceEvidence(ctx, q, at.Add(-time.Second))
		if e != nil || len(values) != 1 {
			t.Fatal(values, e)
		}
		got, e := md.SelectValuationPrice(q, values)
		if e != nil || got.Fact == nil || got.Fact.Price != want || got.ObservationRevision != revision {
			t.Fatal(got, e)
		}
	}
	check("101.000000000", 2)
	q.KnowledgeAt = first[0].ReceivedAt
	check("100.000000000", 1)
	q.KnowledgeAt = first[0].ReceivedAt.Add(-time.Microsecond)
	values, e := repo.PriceEvidence(ctx, q, at)
	if e != nil || len(values) != 0 {
		t.Fatal("future revision leaked", values, e)
	}
	q.KnowledgeAt = time.Time{}
	q.AccessScope = "other"
	values, e = repo.PriceEvidence(ctx, q, at)
	if e != nil || len(values) != 0 {
		t.Fatal("scope leaked", values, e)
	}
	q.Scope = scope
	if e = repo.ConfigureSource(ctx, scope, "Personal", false); e != nil {
		t.Fatal(e)
	}
	values, e = repo.PriceEvidence(ctx, q, at)
	if e != nil || len(values) != 0 {
		t.Fatal("disabled source leaked", values, e)
	}

	if e = repo.ConfigureSource(ctx, scope, "Personal", true); e != nil {
		t.Fatal(e)
	}
	later := o
	later.At = at.Add(30 * time.Second)
	later.Price = "102"
	future := o
	future.At = at.Add(2 * time.Minute)
	future.Price = "9999"
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{later, future}); e != nil {
		t.Fatal(e)
	}
	latest, e := readPriceEvidenceMode(ctx, pool.DB, q, at.Add(-time.Second), true)
	if e != nil || len(latest) != 1 || !latest[0].At.Equal(later.At) || latest[0].Price != "102.000000000" {
		t.Fatal("latest price crossed time/source boundary", latest, e)
	}
	later.Price = "103"
	corrected, e := repo.SaveObservations(ctx, scope, []md.Observation{later})
	if e != nil {
		t.Fatal(e)
	}
	latest, e = readPriceEvidenceMode(ctx, pool.DB, q, at.Add(-time.Second), true)
	if e != nil || len(latest) != 1 || latest[0].Revision != 2 || latest[0].Price != "103.000000000" || !latest[0].ReceivedAt.Equal(corrected[0].ReceivedAt) {
		t.Fatal("latest correction ignored", latest, e)
	}
}
