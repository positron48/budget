package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestMarketDividendSnapshotsAtomicScopedImmutable(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	r := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if err := r.ConfigureSource(ctx, scope, "Synthetic dividends", true); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	q := md.JobRequest{Kind: "dividend_facts", InstrumentID: uuid.NewString(), From: start, To: start.AddDate(0, 0, 30)}
	f := md.DividendFacts{Instrument: md.Instrument{ID: q.InstrumentID, Ticker: "TEST", Name: "Synthetic", Kind: "share", Currency: "RUB", Detailed: true}, From: q.From, To: q.To, AsOf: start, Dividends: []md.Dividend{
		{RecordDate: start.AddDate(0, 0, 2), PaymentDate: start.AddDate(0, 0, 90), DividendType: "Regular Cash"},
		{RecordDate: start.AddDate(0, 0, 20), DividendType: "Cancelled", DividendNet: &md.BondAmount{Amount: "0", Currency: "USD"}},
	}}
	svc := md.NewService(r, NewTenantRepo(pool))
	job, err := svc.Refresh(ctx, tenant, actor, scope, q)
	if err != nil {
		t.Fatal(err)
	}
	dup, err := svc.Refresh(ctx, tenant, actor, scope, q)
	if err != nil || dup.ID != job.ID {
		t.Fatal("duplicate load", dup, err)
	}
	job, err = r.Claim(ctx, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wrong := job
	wrong.Scope.AccessScope = "another"
	if err = r.CompleteDividendFacts(ctx, wrong, f); !errors.Is(err, md.ErrLeaseLost) {
		t.Fatal("foreign lease", err)
	}
	if err = r.Complete(ctx, job, nil); !errors.Is(err, md.ErrInvalid) {
		t.Fatal("wrong completion path", err)
	}
	if _, err = pool.DB.Exec(ctx, `CREATE FUNCTION reject_dividend_coverage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic coverage failure'; END $$; CREATE TRIGGER reject_dividend_coverage BEFORE INSERT ON market_data_coverage FOR EACH ROW EXECUTE FUNCTION reject_dividend_coverage()`); err != nil {
		t.Fatal(err)
	}
	if err = r.CompleteDividendFacts(ctx, job, f); err == nil {
		t.Fatal("publication should fail")
	}
	if got, e := r.DividendFacts(ctx, scope, q); e != nil || got != nil {
		t.Fatal("partial snapshot published", got, e)
	}
	if _, err = pool.DB.Exec(ctx, `DROP TRIGGER reject_dividend_coverage ON market_data_coverage`); err != nil {
		t.Fatal(err)
	}
	if err = r.CompleteDividendFacts(ctx, job, f); err != nil {
		t.Fatal(err)
	}
	if err = r.CompleteDividendFacts(ctx, job, f); !errors.Is(err, md.ErrLeaseLost) {
		t.Fatal("completed lease reused", err)
	}
	narrow := q
	narrow.To = start.AddDate(0, 0, 3)
	got, err := svc.DividendFacts(ctx, tenant, actor, scope, narrow)
	if err != nil || got == nil || len(got.Dividends) != 1 || got.Dividends[0].DividendNet != nil || !got.Dividends[0].PaymentDate.After(narrow.To) {
		t.Fatal("record-date filter", got, err)
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE market_dividend_snapshots SET payload_hash='changed' WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("source snapshot changed")
	}
	foreign := scope
	foreign.TenantID = uuid.NewString()
	if _, err = svc.DividendFacts(ctx, tenant, actor, foreign, q); !errors.Is(err, md.ErrForbidden) {
		t.Fatal("tenant leak", err)
	}
	foreign = scope
	foreign.AccessScope = "another"
	if got, err = r.DividendFacts(ctx, foreign, q); err != nil || got != nil {
		t.Fatal("entitlement leak", got, err)
	}
	if err = r.ConfigureSource(ctx, scope, "Disabled", false); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.DividendFacts(ctx, tenant, actor, scope, q); !errors.Is(err, md.ErrNotFound) {
		t.Fatal("disabled source exposed", err)
	}
	if err = r.ConfigureSource(ctx, scope, "Restored", true); err != nil {
		t.Fatal(err)
	}
	job, err = r.Enqueue(ctx, scope, q)
	if err != nil {
		t.Fatal(err)
	}
	job, err = r.Claim(ctx, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	f.Dividends = nil // An explicit empty revision is not absence of any snapshot.
	if err = r.CompleteDividendFacts(ctx, job, f); err != nil {
		t.Fatal(err)
	}
	got, err = r.DividendFacts(ctx, scope, q)
	if err != nil || got == nil || got.ID != job.ID || len(got.Dividends) != 0 {
		t.Fatal("latest empty correction", got, err)
	}
	var journal, versions int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1`, tenant).Scan(&journal); err != nil || journal != 0 {
		t.Fatal("source load posted cash", journal, err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_dividend_snapshots WHERE tenant_id=$1`, tenant).Scan(&versions); err != nil || versions != 2 {
		t.Fatal("revisions lost", versions, err)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0058_market_dividend_facts")
}
