package postgres

import (
	"context"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"strings"
	"testing"
	"time"
)

func TestInvestmentProfitSettlementDoesNotRequireExtraFX(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	if _, e := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Profit','RUB')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", at, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	for i, op := range []d.InvestmentOperation{{Kind: "deposit", Currency: "RUB", Amount: "1000", OccurredAt: at}, {Kind: "fx", Currency: "RUB", Amount: "100", ToCurrency: "USD", ToAmount: "10", OccurredAt: at.Add(time.Hour), SettlementAt: at.AddDate(0, 0, 1)}} {
		op.LedgerID = ledger.ID
		if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(i + 1), Operation: op}); e != nil {
			t.Fatal(e)
		}
	}
	repo := NewMarketDataRepo(pool)
	policy := md.FXPolicy{Key: "market", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	for i, rate := range map[int]string{0: "10", 2: "20"} {
		day := at.AddDate(0, 0, i)
		if _, e = repo.SaveFXPoints(ctx, tenant, "market", 1, []md.FXPoint{{From: "USD", To: "RUB", Rate: rate, At: day, AvailableAt: day, ValidUntil: day.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: uuid.NewString()}}); e != nil {
			t.Fatal(e)
		}
	}
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "USD", FXPolicyKey: "market"}
	if _, e = repo.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-03"); e != nil {
		t.Fatal(e)
	}
	for _, date := range []string{"2024-01-02", "2024-01-03"} {
		if _, e = repo.BuildDailyNAV(ctx, q, date); e != nil {
			t.Fatal(e)
		}
	}
	key, _, _ := navSeries(q)
	out, e := repo.ProfitForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-03")
	if e != nil || out.Performance.Profit == nil || out.Performance.Profit.RatString() != "-45" || !out.Attribution.Complete || out.Timeline.Points[1].Cumulative != nil || out.Timeline.Points[2].Cumulative.RatString() != "-45" {
		t.Fatal(out, e)
	}
	if !out.Instruments.Complete || len(out.Instruments.Rows) != 0 || out.Instruments.Remainder.Known.RatString() != "-45" {
		t.Fatal(out.Instruments)
	}
	for _, c := range out.Attribution.Components {
		want := "0"
		if c.Kind == "cash_fx" {
			want = "-45"
		}
		if c.Known.RatString() != want {
			t.Fatal(c, want)
		}
	}
}
