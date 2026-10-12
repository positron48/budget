package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	av "github.com/positron48/budget/internal/adapter/alphavantage"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/usecase/alphafx"
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentAlphaFXAtomicRetrospective(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor, foreign string
	for _, target := range []*string{&tenant, &foreign} {
		if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Alpha FX','RUB') RETURNING id`).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('alpha-fx@example.test','test') RETURNING id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	policy := md.FXPolicy{Key: "alpha-retrospective", Mode: "market", Source: "alphavantage", AccessScope: "personal", SourceReference: "Synthetic personal FX_DAILY", TimeConvention: md.AlphaTimeConvention, ValueConvention: md.AlphaValueConvention, ValidityConvention: md.AlphaValidityConvention, Alpha: &md.AlphaFXPolicy{From: day, Convention: md.AlphaRetrospectiveConvention}}
	repo := NewMarketDataRepo(pool)
	if _, err := repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); err != nil {
		t.Fatal(err)
	}
	audits := []av.Audit{}
	for _, pair := range []string{"USD/RUB", "EUR/RUB"} {
		audits = append(audits, av.Audit{Source: "alphavantage", Pair: pair, From: "2024-01-05", To: "2024-01-07", Timezone: "UTC", LastRefreshed: "2024-01-05", BodySHA256: strings.Repeat("a", 64), RetrievedAt: day.Add(96 * time.Hour), Bars: []av.Bar{{Date: "2024-01-05", Open: "90", High: "100", Low: "80", Close: "90"}}})
	}
	bad := append([]av.Audit(nil), audits...)
	bad[1].Pair = "EUR/USD"
	if _, err := repo.SaveAlphaFX(ctx, tenant, policy.Key, 1, bad); err == nil {
		t.Fatal("bad second series accepted")
	}
	var count int
	if err := pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_fx_point_revisions WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial write", count, err)
	}
	for i := 0; i < 2; i++ {
		n, err := repo.SaveAlphaFX(ctx, tenant, policy.Key, 1, audits)
		if err != nil || n != 2 {
			t.Fatal(n, err)
		}
	}
	if err := pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_fx_point_revisions WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 2 {
		t.Fatal("retry changed revisions", count, err)
	}
	workerSource := &alphaWorkerFixture{repo: repo, audits: audits}
	worker := alphafx.Service{Source: workerSource, Store: repo, Tenant: tenant, Key: policy.Key}
	result, e := worker.Sync(ctx, time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC))
	if e != nil || result.Deferred || result.Points != 2 || workerSource.calls != 2 {
		t.Fatal("worker to PostgreSQL publication", result, e)
	}
	result, e = worker.Sync(ctx, time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC))
	if e != nil || !result.Deferred || workerSource.calls != 2 {
		t.Fatal("worker restart quota", result, e)
	}
	refreshed := append([]av.Audit(nil), audits...)
	for i := range refreshed {
		refreshed[i].RetrievedAt = refreshed[i].RetrievedAt.Add(time.Hour)
		refreshed[i].BodySHA256 = strings.Repeat("b", 64)
	}
	if _, err := repo.SaveAlphaFX(ctx, tenant, policy.Key, 1, refreshed); err != nil {
		t.Fatal(err)
	}
	if err := pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_fx_point_revisions WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 2 {
		t.Fatal("metadata-only refresh dirtied fixing history", count, err)
	}
	corrected := append([]av.Audit(nil), refreshed...)
	corrected[0].Bars = append([]av.Bar(nil), refreshed[0].Bars...)
	corrected[0].Bars[0].Close = "91"
	if _, err := repo.SaveAlphaFX(ctx, tenant, policy.Key, 1, corrected); err != nil {
		t.Fatal(err)
	}
	if err := pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_fx_point_revisions WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 3 {
		t.Fatal("actual price correction must revise only one fixing", count, err)
	}
	if _, err := repo.SaveAlphaFX(ctx, tenant, policy.Key, 1, audits); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		at                      time.Time
		fresh, expired, missing int64
	}{{day.Add(12 * time.Hour), 0, 0, 2}, {day.Add(48 * time.Hour), 2, 0, 0}, {day.Add(144 * time.Hour), 0, 2, 0}} {
		h, e := repo.ReadAlphaFXWorkerHealth(ctx, tenant, policy.Key, tc.at)
		if e != nil || h.SourceEnabled == nil || !*h.SourceEnabled || h.FXStates["fresh"] != tc.fresh || h.FXStates["expired"] != tc.expired || h.FXStates["missing"] != tc.missing {
			t.Fatal("persisted FX freshness", h, e)
		}
	}
	if _, err := repo.SaveAlphaFX(ctx, tenant, policy.Key, 2, audits); !errors.Is(err, md.ErrConflict) {
		t.Fatal("version fencing", err)
	}
	if _, err := repo.SaveAlphaFX(ctx, foreign, policy.Key, 1, audits); !errors.Is(err, md.ErrNotFound) {
		t.Fatal("tenant isolation", err)
	}
	_, saved, err := repo.ReadFXSnapshot(ctx, tenant, policy.Key, day.Add(48*time.Hour), time.Time{})
	if err != nil || len(saved) != 2 {
		t.Fatal(saved, err)
	}
	fixings := make([]calc.FXFixing, len(saved))
	for i, v := range saved {
		fixings[i] = v.Fixing()
	}
	resolver, err := calc.NewFXResolver(fixings, md.FXPolicyRef(policy.Key, 1), day.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	cross, ok := resolver.Resolve("EUR", "USD")
	if !ok || cross.Rate.RatString() != "1" || len(cross.Legs) != 2 {
		t.Fatal("RUB hub", cross, ok)
	}
	// Exercise the actual historical NAV path: today's retrieved checkpoint
	// prices past cash only under the explicitly retrospective convention.
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Alpha cash fixture", day, "alpha-ledger")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "alpha-deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "USD", Amount: "10", OccurredAt: day.Add(time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: policy.Key}
	for _, tc := range []struct {
		date     string
		complete bool
		value    string
	}{{"2024-01-05", false, "0"}, {"2024-01-06", true, "900"}, {"2024-01-10", false, "0"}} {
		v, e := repo.BuildDailyNAV(ctx, q, tc.date)
		if e != nil || v.Result == nil || v.Result.Complete != tc.complete || v.Result.KnownValue.RatString() != tc.value {
			t.Fatal("historical NAV", tc, v, e)
		}
	}
	// A later complete snapshot omitting an old date is refused atomically.
	more := append([]av.Audit(nil), audits...)
	more[0].Bars = append(append([]av.Bar(nil), audits[0].Bars...), av.Bar{Date: "2024-01-06", Open: "90", High: "100", Low: "80", Close: "90"})
	if _, err := repo.SaveAlphaFX(ctx, tenant, policy.Key, 1, more); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveAlphaFX(ctx, tenant, policy.Key, 1, audits); !errors.Is(err, md.ErrInvalid) {
		t.Fatal("omitted old date was silently accepted", err)
	}
	// Revisions must retain the new typed convention for later reads/exports.
	read, _, err := repo.ReadFXSnapshot(ctx, tenant, policy.Key, time.Now(), time.Time{})
	if err != nil || read.Alpha == nil || read.Alpha.Convention != md.AlphaRetrospectiveConvention {
		t.Fatal(read, err)
	}
	bundle, err := NewInvestmentRepo(pool).ExportInvestmentArchive(ctx, tenant, actor)
	if err != nil {
		t.Fatal(err)
	}
	target, cleanupTarget := withPg(t)
	defer cleanupTarget()
	if _, err = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Alpha restore','RUB')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'alpha-restore@example.test','test')`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = NewInvestmentRepo(target).RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); err != nil {
		t.Fatal(err)
	}
	restoredRepo := NewMarketDataRepo(target)
	restoredPolicy, restoredPoints, err := restoredRepo.ReadFXSnapshot(ctx, tenant, policy.Key, time.Now(), time.Time{})
	if err != nil || restoredPolicy.Alpha == nil || len(restoredPoints) != 0 {
		t.Fatal("restored policy / excluded licensed cache", err)
	}
	missing, err := restoredRepo.BuildDailyNAV(ctx, q, "2024-01-06")
	if err != nil || missing.Result == nil || missing.Result.Complete {
		t.Fatal("restore invented market cache", err)
	}
	if _, err = restoredRepo.SaveAlphaFX(ctx, tenant, policy.Key, 1, audits); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := restoredRepo.BuildDailyNAV(ctx, q, "2024-01-06")
	if err != nil || rebuilt.Result == nil || !rebuilt.Result.Complete || rebuilt.Result.KnownValue.RatString() != "900" {
		t.Fatal("restored Alpha NAV after explicit cache reload", err)
	}

}

type alphaWorkerFixture struct {
	repo   *MarketDataRepo
	audits []av.Audit
	calls  int
}

func (f *alphaWorkerFixture) BeginBatch(ctx context.Context) (bool, error) {
	return f.repo.ReserveAlphaFXBatch(ctx, strings.Repeat("d", 64))
}
func (f *alphaWorkerFixture) History(_ context.Context, pair, from, to string) (av.Audit, error) {
	f.calls++
	for _, audit := range f.audits {
		if audit.Pair == pair && audit.From == from && audit.To == to {
			return audit, nil
		}
	}
	return av.Audit{}, md.ErrInvalid
}
