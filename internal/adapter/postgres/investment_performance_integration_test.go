package postgres

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentPerformanceSnapshotAndFlows(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Performance','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('performance@example.test','test') RETURNING id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Broker", day, "ledger")
	if err != nil {
		t.Fatal(err)
	}
	version := int64(1)
	mutate := func(kind, amount, key string, at time.Time) {
		t.Helper()
		_, e := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: key, ExpectedLedgerVersion: version, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: kind, Currency: "RUB", Amount: amount, OccurredAt: at, InstrumentKey: func() string {
			if kind == "dividend" {
				return "stock"
			}
			return ""
		}()}})
		if e != nil {
			t.Fatal(e)
		}
		version++
	}
	mutate("deposit", "1000", "initial", day.Add(time.Hour))
	mutate("deposit", "500", "contribution", day.Add(25*time.Hour))
	mutate("dividend", "20", "income", day.Add(26*time.Hour))
	mutate("fee", "2", "fee", day.Add(49*time.Hour))
	repo := NewMarketDataRepo(pool)
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market"}
	if _, err = repo.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-03"); err != nil {
		t.Fatal(err)
	}
	build := func() {
		t.Helper()
		for _, date := range []string{"2024-01-02", "2024-01-03"} {
			if _, e := repo.BuildDailyNAV(ctx, q, date); e != nil {
				t.Fatal(e)
			}
		}
	}
	build()
	key, _, _ := navSeries(q)
	result, err := repo.PerformanceForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-03")
	if err != nil || result.Profit == nil || result.Profit.Cmp(big.NewRat(18, 1)) != 0 || result.NetContributions.Cmp(big.NewRat(500, 1)) != 0 || len(result.Flows) != 1 || result.TWR.Return == nil || result.XIRR.Rate == nil {
		t.Fatalf("performance: %+v %v", result, err)
	}
	profit, e := repo.ProfitForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-03")
	if e != nil || !profit.Attribution.Complete || profit.Timeline.Points[2].Cumulative.RatString() != "18" {
		t.Fatal(profit, e)
	}
	want := map[string]string{"earned_income": "20", "fees": "-2", "cash_fx": "0", "fx_residual": "0"}
	for _, c := range profit.Attribution.Components {
		if expected, exists := want[c.Kind]; exists && c.Known.RatString() != expected {
			t.Fatal(c, expected)
		}
	}
	// Baseline outside a watch need not have its own published projection.
	boundary, err := repo.PerformanceForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-03")
	if err != nil || boundary.Profit == nil || boundary.Profit.Cmp(big.NewRat(18, 1)) != 0 {
		t.Fatalf("missing baseline projection: %+v %v", boundary, err)
	}
	// A shared repeatable-read snapshot retains both old payments and old NAV.
	tx, err := pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	mutate("deposit", "100", "late-correction", day.Add(27*time.Hour))
	old, err := readInvestmentPerformance(ctx, tx, tenant, actor, key, "2024-01-02", "2024-01-03")
	if err != nil || old.Generation != generation || old.NetContributions == nil || old.NetContributions.Cmp(big.NewRat(500, 1)) != 0 || old.Profit.Cmp(big.NewRat(18, 1)) != 0 {
		t.Fatalf("mixed snapshots: %+v %v", old, err)
	}
	oldProfit, e := readInvestmentProfit(ctx, tx, tenant, actor, key, "2024-01-02", "2024-01-03")
	if e != nil || !oldProfit.Attribution.Complete || oldProfit.Performance.Profit.RatString() != "18" {
		t.Fatal("mixed profit snapshot", oldProfit, e)
	}
	freshProfit, e := repo.ProfitForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-03")
	if e != nil || freshProfit.Attribution.Complete || freshProfit.Performance.Profit != nil {
		t.Fatal("stale total used for attribution", freshProfit, e)
	}
	if _, e = repo.ProfitForSeries(ctx, uuid.NewString(), actor, key, "2024-01-02", "2024-01-03"); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal(e)
	}
	fresh, err := repo.PerformanceForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-03")
	if err != nil || fresh.Profit != nil || fresh.XIRR.Rate != nil || fresh.TWR.Return != nil || fresh.Reason != "terminal_nav_unknown" {
		t.Fatalf("stale NAV used: %+v %v", fresh, err)
	}
	build()
	fresh, err = repo.PerformanceForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-03")
	if err != nil || fresh.NetContributions == nil || fresh.NetContributions.Cmp(big.NewRat(600, 1)) != 0 || fresh.Profit.Cmp(big.NewRat(18, 1)) != 0 {
		t.Fatalf("rebuilt performance: %+v %v", fresh, err)
	}
	if _, err = repo.PerformanceForSeries(ctx, tenant, uuid.NewString(), key, "2024-01-02", "2024-01-03"); !errors.Is(err, d.ErrInvestmentNotFound) {
		t.Fatal("foreign actor", err)
	}
	if _, err = repo.PerformanceForSeries(ctx, uuid.NewString(), actor, key, "2024-01-02", "2024-01-03"); !errors.Is(err, d.ErrInvestmentNotFound) {
		t.Fatal("foreign tenant", err)
	}
	if _, err = repo.PerformanceForSeries(ctx, tenant, actor, key, "2023-12-31", "2024-01-03"); !errors.Is(err, d.ErrInvestmentInvalid) {
		t.Fatal("unregistered range", err)
	}
	// Different historical rates must be applied to NAV and each cash flow.
	policy := md.FXPolicy{Key: "performance-fx", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic test evidence", TimeConvention: "Published timestamps", ValueConvention: "RUB per USD", ValidityConvention: "Explicit validity"}
	if _, err = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); err != nil {
		t.Fatal(err)
	}
	fx := []md.FXPoint{}
	for i, rate := range []string{"80", "100", "120"} {
		at := day.AddDate(0, 0, i)
		fx = append(fx, md.FXPoint{From: "USD", To: "RUB", Rate: rate, At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: at.Format(time.DateOnly)})
	}
	if _, err = repo.SaveFXPoints(ctx, tenant, policy.Key, 1, fx); err != nil {
		t.Fatal(err)
	}
	usd := q
	usd.Currency = "USD"
	usd.FXPolicyKey = policy.Key
	if _, err = repo.EnqueueDailyNAV(ctx, usd, "2024-01-02", "2024-01-03"); err != nil {
		t.Fatal(err)
	}
	rebuildUSD := func() {
		t.Helper()
		for _, date := range []string{"2024-01-02", "2024-01-03"} {
			if _, err = repo.BuildDailyNAV(ctx, usd, date); err != nil {
				t.Fatal(err)
			}
		}
	}
	rebuildUSD()
	usdKey, _, _ := navSeries(usd)
	converted, err := repo.PerformanceForSeries(ctx, tenant, actor, usdKey, "2024-01-02", "2024-01-03")
	if err != nil || converted.NetContributions == nil || converted.NetContributions.Cmp(big.NewRat(6, 1)) != 0 || converted.Profit.Cmp(big.NewRat(-301, 60)) != 0 || len(converted.Flows) != 2 || len(converted.Flows[0].FX) != 1 || !converted.Flows[0].FX[0].Inverse {
		t.Fatalf("historical FX: %+v %v", converted, err)
	}
	fx[1].ValidUntil = fx[1].At.Add(time.Hour)
	fx[1].EvidenceSHA256 = strings.Repeat("b", 64)
	if _, err = repo.SaveFXPoints(ctx, tenant, policy.Key, 1, fx[1:2]); err != nil {
		t.Fatal(err)
	}
	rebuildUSD()
	unknown, err := repo.PerformanceForSeries(ctx, tenant, actor, usdKey, "2024-01-02", "2024-01-03")
	if err != nil || unknown.NetContributions != nil || unknown.Profit != nil || unknown.XIRR.Rate != nil || unknown.FlowReason != "external_flow_unknown" {
		t.Fatalf("expired fixing resurrected: %+v %v", unknown, err)
	}

}
