package postgres

import (
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"github.com/positron48/budget/migrations"
)

func TestInvestmentPortfolioTrackingPersistenceWithoutFunding(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Portfolio", StartedAt: at, PortfolioTracking: true}, "portfolio")
	if err != nil || !ledger.PortfolioTracking {
		t.Fatal(ledger, err)
	}
	version := ledger.Version
	for i, operation := range []d.InvestmentOperation{
		{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "ABC", Currency: "RUB", Quantity: "2", Amount: "200", Fee: "2", OccurredAt: at.Add(time.Hour)},
		{LedgerID: ledger.ID, Kind: "coupon", InstrumentKey: "ABC", Currency: "RUB", Amount: "15", TaxReviewRequired: true, OccurredAt: at.Add(2 * time.Hour)},
	} {
		key := []string{"buy", "coupon"}[i]
		mutation := inv.Mutation{Action: "create", RequestKey: key, Operation: operation, ExpectedLedgerVersion: version}
		preview, e := svc.PreviewMutation(ctx, tenant, actor, mutation)
		if e != nil || !preview.Valid {
			t.Fatal(preview, e)
		}
		mutation.PreviewHash = preview.Hash
		if _, e = svc.Mutate(ctx, tenant, actor, mutation); e != nil {
			t.Fatal(e)
		}
		version++
	}
	// A new repository read proves both the persisted mode and replay behavior.
	state, err := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool)).Snapshot(ctx, tenant, actor, ledger.ID, at.AddDate(0, 0, 1))
	if err != nil || !state.Ledger.PortfolioTracking || len(state.Cash) != 0 || len(state.Positions) != 1 {
		t.Fatal(state, err)
	}
	if state.Positions[0].Quantity != "2" || state.Positions[0].CleanCost != "200" || state.Positions[0].CouponEarned != "15" {
		t.Fatal(state.Positions)
	}
	// Rollback must refuse to reinterpret a populated unfunded tracker as a cash book.
	down, err := migrations.FS.ReadFile("0060_investment_portfolio_tracking.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, string(down)); err == nil {
		t.Fatal("rollback accepted populated tracker")
	}
	if _, err = svc.Snapshot(ctx, tenant, actor, ledger.ID, at.AddDate(0, 0, 1)); err != nil {
		t.Fatal("failed rollback changed live schema", err)
	}
}

func TestInvestmentPortfolioUpgradeExistingStandaloneAccount(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Existing", StartedAt: at}, "existing")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "funding", ExpectedLedgerVersion: ledger.Version, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "1000", OccurredAt: at.Add(time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrations.FS.ReadFile("0060_investment_portfolio_tracking.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	var before string
	if err = pool.DB.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(o) ORDER BY id)::text FROM investment_operations o WHERE ledger_id=$1`, ledger.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	up, err := migrations.FS.ReadFile("0060_investment_portfolio_tracking.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	state, err := svc.Snapshot(ctx, tenant, actor, ledger.ID, at.AddDate(0, 0, 1))
	if err != nil || !state.Ledger.PortfolioTracking || len(state.Cash) != 0 {
		t.Fatal(state, err)
	}
	var after string
	if err = pool.DB.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(o) ORDER BY id)::text FROM investment_operations o WHERE ledger_id=$1`, ledger.ID).Scan(&after); err != nil || after != before {
		t.Fatal("source operations changed", err)
	}
	var generation, repeat int64
	if err = pool.DB.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&repeat); err != nil || repeat != generation {
		t.Fatal("repeat migration changed state", err)
	}
}
