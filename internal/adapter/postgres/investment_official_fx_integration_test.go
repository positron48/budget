package postgres

import (
	"encoding/json"
	"errors"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"os"
	"strings"
	"testing"
	"time"
)

func officialPolicyFixture(from time.Time) md.FXPolicy {
	return md.FXPolicy{Key: "cbr", Mode: "cbr", Source: "cbr", AccessScope: "public", SourceReference: "https://www.cbr.ru/currency_base/", TimeConvention: "Effective Moscow calendar date", ValueConvention: "RUB per one currency unit", ValidityConvention: "One verified calendar day", Official: &md.OfficialFXPolicy{From: from, Convention: "effective_moscow_date_v1"}}
}
func TestOfficialFXAtomicPublicationHistoricalNAVAndIsolation(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	day := time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC)
	policy := officialPolicyFixture(day)
	if _, e := repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	var points []md.FXPoint
	for i := 0; i < 501; i++ {
		for _, code := range []string{"USD", "EUR"} {
			at := day.AddDate(0, 0, i).Add(-3 * time.Hour)
			points = append(points, md.FXPoint{From: code, To: "RUB", Rate: "80", At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: "Synthetic CBR record"})
		}
	}
	if e := repo.SaveOfficialFX(ctx, tenant, "cbr", 1, points); e != nil {
		t.Fatal(e)
	}
	if e := repo.SaveOfficialFX(ctx, tenant, "cbr", 1, points); e != nil {
		t.Fatal(e)
	}
	// Failure on point 1002 rolls back changes in the previous internal batch too.
	for i := range points {
		points[i].Rate = "90"
	}
	trigger := `CREATE FUNCTION reject_official_fx() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.affected_at='` + points[len(points)-1].At.Format(time.RFC3339) + `'::timestamptz THEN RAISE EXCEPTION 'late batch failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_official_fx BEFORE INSERT ON investment_fx_changes FOR EACH ROW EXECUTE FUNCTION reject_official_fx()`
	if _, e := pool.DB.Exec(ctx, trigger); e != nil {
		t.Fatal(e)
	}
	if e := repo.SaveOfficialFX(ctx, tenant, "cbr", 1, points); e == nil {
		t.Fatal("partial multi-batch publication")
	}
	_, saved, e := repo.ReadFXSnapshot(ctx, tenant, "cbr", points[0].At, time.Time{})
	if e != nil || len(saved) != 2 || saved[0].Rate != "80" || saved[0].Revision != 1 {
		t.Fatal("rollback/retry", e, saved)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER reject_official_fx ON investment_fx_changes; DROP FUNCTION reject_official_fx()`); e != nil {
		t.Fatal(e)
	}
	if e = repo.SaveOfficialFX(ctx, tenant, "cbr", 1, points); e != nil {
		t.Fatal(e)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "USD", day, "create")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "USD", Amount: "10", OccurredAt: day}}); e != nil {
		t.Fatal(e)
	}
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "cbr", At: day.Add(12 * time.Hour)}
	value, e := repo.ReadInvestmentValuation(ctx, q)
	if e != nil || !value.Complete || value.KnownValue.RatString() != "900" {
		t.Fatal("historical CBR NAV", e, value)
	}
	q.FXPolicyKey = "market"
	value, e = repo.ReadInvestmentValuation(ctx, q)
	if e != nil || value.Complete {
		t.Fatal("CBR leaked into market mode", e, value)
	}
	q.FXPolicyKey = "cbr"
	built, e := repo.BuildDailyNAV(ctx, q, "2019-10-01")
	if e != nil || built.State != "ready" || built.Result.KnownValue.RatString() != "900" {
		t.Fatal("official daily NAV", e, built)
	}
	points[0].Rate = "95"
	if e = repo.SaveOfficialFX(ctx, tenant, "cbr", 1, points); e != nil {
		t.Fatal(e)
	}
	history, e := repo.DailyNAVHistory(ctx, q, "2019-10-01", "2019-10-01")
	if e != nil || len(history) != 1 || history[0].State != "stale" || history[0].Result != nil {
		t.Fatal("official correction did not invalidate NAV", e, history)
	}
	built, e = repo.BuildDailyNAV(ctx, q, "2019-10-01")
	if e != nil || built.State != "ready" || built.Result.KnownValue.RatString() != "950" {
		t.Fatal("official daily NAV not rebuilt", e, built)
	}
	duplicate := append([]md.FXPoint(nil), points...)
	duplicate[len(duplicate)-1] = duplicate[0]
	if e = repo.SaveOfficialFX(ctx, tenant, "cbr", 1, duplicate); !errors.Is(e, md.ErrInvalid) {
		t.Fatal("cross-batch duplicate accepted", e)
	}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 1); e != nil {
		t.Fatal(e)
	}
	if e = repo.SaveOfficialFX(ctx, tenant, "cbr", 1, points); !errors.Is(e, md.ErrConflict) {
		t.Fatal("stale importer accepted", e)
	}
	bundle, e := NewInvestmentRepo(pool).ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal(e)
	}
}

func TestOfficialFXPublicHistoryFixture(t *testing.T) {
	path := os.Getenv("BUDGET_CBR_TEST_POINTS")
	if path == "" {
		t.Skip("set BUDGET_CBR_TEST_POINTS to verified public CBR JSON")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var points []md.FXPoint
	if e = json.Unmarshal(raw, &points); e != nil || len(points) == 0 {
		t.Fatal(e)
	}
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	from, to := points[0].At.Add(3*time.Hour), points[0].At.Add(3*time.Hour)
	for _, p := range points {
		day := p.At.Add(3 * time.Hour)
		if day.Before(from) {
			from = day
		}
		if day.After(to) {
			to = day
		}
	}
	policy := officialPolicyFixture(from)
	if e = md.ValidateOfficialFXPoints(policy, points, from, to); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	if e = repo.SaveOfficialFX(ctx, tenant, "cbr", 1, points); e != nil {
		t.Fatal(e)
	}
	_, saved, e := repo.ReadFXSnapshot(ctx, tenant, "cbr", to.Add(12*time.Hour), time.Time{})
	if e != nil || len(saved) != len(points) {
		t.Fatal(e, len(saved), len(points))
	}
	// Independent HTML control point: CBR's 01.10.2019 table gives USD 64.6407.
	if from.Format(time.DateOnly) != "2019-10-01" {
		t.Fatal("fixture must cover the original history start")
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Public CBR control", from, "create")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "USD", Amount: "10", OccurredAt: from}}); e != nil {
		t.Fatal(e)
	}
	value, e := repo.ReadInvestmentValuation(ctx, nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "cbr", At: from.Add(12 * time.Hour)})
	if e != nil || !value.Complete || value.KnownValue.RatString() != "646407/1000" {
		t.Fatal("public CBR historical NAV differs from independent HTML table", e, value)
	}
	t.Logf("Verified public CBR history in isolated PostgreSQL: %s..%s, %d daily fixings", from.Format(time.DateOnly), to.Format(time.DateOnly), len(saved))
}
