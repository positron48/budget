package postgres

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	av "github.com/positron48/budget/internal/adapter/alphavantage"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/usecase/alphafx"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// Explicit opt-in: two real read-only FX requests; key stays in Keychain/client.
// Only disposable PostgreSQL and a synthetic cash ledger are written. Never CI.
func TestInvestmentAlphaFXLiveRefresh(t *testing.T) {
	if os.Getenv("INVESTMENT_ALPHA_LIVE") != "1" {
		t.Skip("live Alpha FX requires explicit opt-in")
	}
	account := os.Getenv("INVESTMENT_ALPHA_KEYCHAIN_ACCOUNT")
	if account == "" {
		t.Fatal("Keychain account required")
	}
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	from := time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC)
	policy := md.FXPolicy{Key: "alpha-retrospective", Mode: "market", Source: "alphavantage", AccessScope: "personal", SourceReference: "User-approved personal retrospective FX_DAILY; live worker read-only acceptance", TimeConvention: md.AlphaTimeConvention, ValueConvention: md.AlphaValueConvention, ValidityConvention: md.AlphaValidityConvention, Alpha: &md.AlphaFXPolicy{From: from, Convention: md.AlphaRetrospectiveConvention}}
	if _, err := repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); err != nil {
		t.Fatal("register live fixture policy")
	}
	client, err := av.NewFromKeychain(ctx, account)
	if err != nil {
		t.Fatal("Alpha credential unavailable")
	}
	client.SetBatchGate(repo.ReserveAlphaFXBatch)
	worker := alphafx.Service{Source: client, Store: repo, Tenant: tenant, Key: policy.Key}
	attempt, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	result, err := worker.Sync(attempt, time.Now().UTC())
	if err != nil || result.Deferred || result.Points < 2 {
		t.Fatal("live worker refresh failed; provider/database diagnostics suppressed")
	}
	repeated, err := worker.Sync(attempt, time.Now().UTC())
	if err != nil || !repeated.Deferred {
		t.Fatal("live worker repeat did not defer")
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	date := today.Add(-24 * time.Hour).Format(time.DateOnly)
	cutoff, err := nav.DayCutoff(date)
	if err != nil {
		t.Fatal(err)
	}
	_, points, err := repo.ReadFXSnapshot(ctx, tenant, policy.Key, cutoff, time.Time{})
	if err != nil {
		t.Fatal("read live FX")
	}
	var latest *md.SavedFXPoint
	for i := range points {
		point := &points[i]
		if point.From == "USD" && point.To == "RUB" && !point.At.After(cutoff) && (latest == nil || point.At.After(latest.At)) {
			latest = point
		}
	}
	if latest == nil || !cutoff.Before(latest.ValidUntil) {
		t.Fatal("latest closed-day USD/RUB unavailable")
	}
	service := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, err := service.CreateLedger(ctx, tenant, actor, "Live FX synthetic cash", from, "live-alpha-ledger")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "live-alpha-cash", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "USD", Amount: "10", OccurredAt: from.Add(time.Hour)}}); err != nil {
		t.Fatal("create synthetic cash")
	}
	value, err := repo.BuildDailyNAV(ctx, nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: policy.Key}, date)
	rate, _ := new(big.Rat).SetString(latest.Rate)
	want := new(big.Rat).Mul(rate, big.NewRat(10, 1))
	if err != nil || value.Result == nil || !value.Result.Complete || value.Result.KnownValue.Cmp(want) != 0 {
		t.Fatal("live-source cash NAV failed")
	}
	health, err := repo.ReadAlphaFXWorkerHealth(ctx, tenant, policy.Key, time.Now().UTC())
	if err != nil || health.FXStates["fresh"] != 2 {
		t.Fatal("live FX freshness degraded")
	}
	t.Logf("Live Alpha FX: points=%d; two pairs; repeat deferred; cash NAV exact; latest two fixings fresh; no production writes", result.Points)
}
