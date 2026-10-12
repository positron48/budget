package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestMarketBondSnapshotsAtomicScopedImmutable(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, user string
	if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Bond','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('bond@example.test','test') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'member')`, tenant, user); err != nil {
		t.Fatal(err)
	}
	r := NewMarketDataRepo(pool)
	s := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	svc := md.NewService(r, NewTenantRepo(pool))
	if err := r.ConfigureSource(ctx, s, "Bond source", true); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	q := md.JobRequest{Kind: "bond_facts", InstrumentID: uuid.NewString(), From: at, To: at.AddDate(0, 0, 30)}
	f := md.BondFacts{Instrument: md.Instrument{ID: q.InstrumentID, Ticker: "TEST", Name: "Bond", Kind: "bond", Currency: "RUB", Detailed: true}, From: q.From, To: q.To, AsOf: at, Terms: md.BondTerms{Nominal: &md.BondAmount{Amount: "9007199254740993.000000001", Currency: "RUB"}}, Coupons: []md.BondCoupon{{Number: 1, Date: at.AddDate(0, 0, 2), Kind: "COUPON_TYPE_FIX"}, {Number: 2, Date: at.AddDate(0, 0, 20), Kind: "COUPON_TYPE_FIX", Pay: &md.BondAmount{Amount: "0", Currency: "RUB"}}}}
	if got, err := svc.BondFacts(ctx, tenant, user, s, q); err != nil || got != nil {
		t.Fatal(got, err)
	}
	first, err := svc.Refresh(ctx, tenant, user, s, q)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := svc.Refresh(ctx, tenant, user, s, q)
	if err != nil || duplicate.ID != first.ID {
		t.Fatal("dedup", duplicate, err)
	}
	j, err := r.Claim(ctx, s, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	bad := f
	bad.To = q.To.Add(time.Second)
	if err = r.CompleteBondFacts(ctx, j, bad); !errors.Is(err, md.ErrInvalid) {
		t.Fatal("range mismatch", err)
	}
	if err = r.Complete(ctx, j, nil); !errors.Is(err, md.ErrInvalid) {
		t.Fatal("facts bypass", err)
	}
	// Forced publication failure must roll back metadata and the snapshot as well.
	if _, err = pool.DB.Exec(ctx, `CREATE FUNCTION fail_bond_coverage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test'; END $$; CREATE TRIGGER fail_bond_coverage BEFORE INSERT ON market_data_coverage FOR EACH ROW EXECUTE FUNCTION fail_bond_coverage()`); err != nil {
		t.Fatal(err)
	}
	if err = r.CompleteBondFacts(ctx, j, f); err == nil {
		t.Fatal("partial publication accepted")
	}
	var count int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_provider_instruments WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("metadata escaped rollback", count, err)
	}
	if got, err := r.BondFacts(ctx, s, q); err != nil || got != nil {
		t.Fatal("snapshot escaped rollback", got, err)
	}
	if _, err = pool.DB.Exec(ctx, `DROP TRIGGER fail_bond_coverage ON market_data_coverage; DROP FUNCTION fail_bond_coverage()`); err != nil {
		t.Fatal(err)
	}
	if err = r.CompleteBondFacts(ctx, j, f); err != nil {
		t.Fatal(err)
	}
	if err = r.CompleteBondFacts(ctx, j, f); !errors.Is(err, md.ErrLeaseLost) {
		t.Fatal("stale lease", err)
	}
	got, err := svc.BondFacts(ctx, tenant, user, s, q)
	if err != nil || got == nil || got.ID != first.ID || got.Terms.Nominal.Amount != f.Terms.Nominal.Amount || got.Coupons[0].Pay != nil || got.Coupons[1].Pay.Amount != "0" {
		t.Fatal("saved evidence", got, err)
	}
	narrow := q
	narrow.From = at.AddDate(0, 0, 10)
	narrow.To = at.AddDate(0, 0, 25)
	got, err = r.BondFacts(ctx, s, narrow)
	if err != nil || got == nil || len(got.Coupons) != 1 || got.Coupons[0].Number != 2 {
		t.Fatal("subrange", got, err)
	}
	wider := q
	wider.To = q.To.Add(time.Second)
	if got, err := r.BondFacts(ctx, s, wider); err != nil || got != nil {
		t.Fatal("partial coverage accepted", got, err)
	}
	other := s
	other.AccessScope = "other"
	if got, err := r.BondFacts(ctx, other, q); err != nil || got != nil {
		t.Fatal("scope leak", got, err)
	}
	if _, err = svc.BondFacts(ctx, tenant, uuid.NewString(), s, q); !errors.Is(err, md.ErrForbidden) {
		t.Fatal("membership bypass", err)
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE market_bond_snapshots SET payload_hash='changed' WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("mutable source")
	}
	if _, err = pool.DB.Exec(ctx, `DELETE FROM market_bond_snapshots WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("deletable source")
	}
	// A corrected response appends a revision and keeps the earlier source intact.
	if _, err = pool.DB.Exec(ctx, `UPDATE market_data_jobs SET created_at=clock_timestamp()-interval '2 minutes' WHERE tenant_id=$1`, tenant); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Refresh(ctx, tenant, user, s, q)
	if err != nil || second.ID == first.ID {
		t.Fatal(second, err)
	}
	j, err = r.Claim(ctx, s, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	f.Coupons = nil
	f.Terms.Nominal.Amount = "500"
	if err = r.CompleteBondFacts(ctx, j, f); err != nil {
		t.Fatal(err)
	}
	got, err = r.BondFacts(ctx, s, q)
	if err != nil || got == nil || got.ID != second.ID || len(got.Coupons) != 0 {
		t.Fatal("empty correction", got, err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_bond_snapshots WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 2 {
		t.Fatal("lost revision", count, err)
	}
	if err = r.ConfigureSource(ctx, s, "Bond source", false); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.BondFacts(ctx, tenant, user, s, q); !errors.Is(err, md.ErrNotFound) {
		t.Fatal("disabled source", err)
	}
	if got, err := r.BondFacts(ctx, s, q); err != nil || got != nil {
		t.Fatal("disabled repository leak", got, err)
	}
}
