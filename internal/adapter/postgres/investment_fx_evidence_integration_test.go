package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentFXEvidence(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, foreign, actor string
	for _, v := range []*string{&tenant, &foreign} {
		if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('FX evidence','RUB') RETURNING id`).Scan(v); e != nil {
			t.Fatal(e)
		}
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('fx-evidence@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	p := md.FXPolicy{Key: "market-test", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic source evidence", TimeConvention: "Explicit published timestamps", ValueConvention: "Quote per one base unit", ValidityConvention: "Explicit verified validity"}
	policy, e := repo.RegisterFXPolicy(ctx, tenant, actor, p, 0)
	if e != nil || policy.Version != 1 {
		t.Fatal(policy, e)
	}
	again, e := repo.RegisterFXPolicy(ctx, tenant, actor, p, 0)
	if e != nil || again.Version != 1 || !again.RecordedAt.Equal(policy.RecordedAt) {
		t.Fatal(again, e)
	}
	if _, e = repo.RegisterFXPolicy(ctx, foreign, actor, p, 0); !errors.Is(e, md.ErrForbidden) {
		t.Fatal("foreign registration", e)
	}
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	point := md.FXPoint{From: "USD", To: "RUB", Rate: "00080.000", At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: "response/day/2020-01-01"}
	first, e := repo.SaveFXPoints(ctx, tenant, p.Key, 1, []md.FXPoint{point})
	if e != nil || first[0].Rate != "80" {
		t.Fatal(first, e)
	}
	point.Rate = "80"
	retry, e := repo.SaveFXPoints(ctx, tenant, p.Key, 1, []md.FXPoint{point})
	if e != nil || retry[0].Revision != 1 {
		t.Fatal(retry, e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			saved, err := repo.SaveFXPoints(ctx, tenant, p.Key, 1, []md.FXPoint{point})
			if err == nil && (len(saved) != 1 || saved[0].Revision != 1) {
				err = errors.New("concurrent identical publication changed revision")
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
	point.Rate = "90"
	point.EvidenceSHA256 = strings.Repeat("b", 64)
	second, e := repo.SaveFXPoints(ctx, tenant, p.Key, 1, []md.FXPoint{point})
	if e != nil || second[0].Revision != 2 {
		t.Fatal(second, e)
	}
	verify := func(knowledge time.Time, want string) {
		t.Helper()
		pol, points, e := repo.ReadFXSnapshot(ctx, tenant, p.Key, at, knowledge)
		if e != nil || len(points) != 1 {
			t.Fatal(pol, points, e)
		}
		got, e := calc.Value([]calc.Component{{ID: "cash", Currency: "USD", Amount: "10", Known: true}}, []calc.FXFixing{points[0].Fixing()}, "RUB", md.FXPolicyRef(pol.Key, pol.Version), at)
		if e != nil || !got.Complete || got.KnownValue.RatString() != want {
			t.Fatal(got, e)
		}
	}
	verify(time.Time{}, "900")
	verify(first[0].RecordedAt, "800")
	if _, _, e = repo.ReadFXSnapshot(ctx, foreign, p.Key, at, time.Time{}); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("tenant leak", e)
	}
	// Failure of downstream invalidation must also undo an otherwise valid correction.
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION reject_fx_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic FX feed failure'; END $$; CREATE TRIGGER reject_fx_change BEFORE INSERT ON investment_fx_changes FOR EACH ROW EXECUTE FUNCTION reject_fx_change()`); e != nil {
		t.Fatal(e)
	}
	point.Rate = "95"
	if _, e = repo.SaveFXPoints(ctx, tenant, p.Key, 1, []md.FXPoint{point}); e == nil {
		t.Fatal("invalidation failure ignored")
	}
	verify(time.Time{}, "900")
	rejectedPolicy := p
	rejectedPolicy.TimeConvention = "Must roll back"
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, rejectedPolicy, 1); e == nil {
		t.Fatal("policy invalidation failure ignored")
	}
	verify(time.Time{}, "900")
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER reject_fx_change ON investment_fx_changes; DROP FUNCTION reject_fx_change()`); e != nil {
		t.Fatal(e)
	}
	// New source convention creates a separate series, not a silent reclassification.
	p.TimeConvention = "Revised session convention"
	updated, e := repo.RegisterFXPolicy(ctx, tenant, actor, p, 1)
	if e != nil || updated.Version != 2 {
		t.Fatal(updated, e)
	}
	pol, points, e := repo.ReadFXSnapshot(ctx, tenant, p.Key, at, time.Time{})
	if e != nil || pol.Version != 2 || len(points) != 0 {
		t.Fatal("old points migrated", pol, points, e)
	}
	verify(first[0].RecordedAt, "800")
	if _, e = repo.SaveFXPoints(ctx, tenant, p.Key, 1, []md.FXPoint{point}); !errors.Is(e, md.ErrConflict) {
		t.Fatal("obsolete worker published", e)
	}
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_fx_policies SET enabled=false WHERE tenant_id=$1 AND policy_key=$2`, tenant, p.Key); e != nil {
		t.Fatal(e)
	}
	if _, _, e = repo.ReadFXSnapshot(ctx, tenant, p.Key, at, first[0].RecordedAt); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("disabled source history leaked", e)
	}
	if _, e = repo.SaveFXPoints(ctx, tenant, p.Key, 2, []md.FXPoint{point}); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("disabled source published", e)
	}
	applyMigrations(t, ctx, pool.DB)
	assertInvestmentRollbackRefused(t, ctx, pool, "0026_investment_fx_evidence")
}
