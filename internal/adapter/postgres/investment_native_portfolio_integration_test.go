package postgres

import (
	"strings"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"github.com/positron48/budget/migrations"
)

func TestInvestmentNativePortfolioPersistsCurrenciesAndGuardsRollback(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Native portfolio", StartedAt: at, PortfolioTracking: true}, "native")
	if e != nil {
		t.Fatal(e)
	}
	version := ledger.Version
	for i, o := range []d.InvestmentOperation{
		{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "BOND", Currency: "RUB", Quantity: "2", Amount: "200", Fee: "2", FeeCurrency: "USD", OccurredAt: at.Add(time.Hour)},
		{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "BOND", Currency: "USD", Quantity: "3", Amount: "30", Fee: "3", FeeCurrency: "RUB", OccurredAt: at.Add(2 * time.Hour)},
		{LedgerID: ledger.ID, Kind: "sell", InstrumentKey: "BOND", Currency: "USD", Quantity: "1", Amount: "15", OccurredAt: at.Add(3 * time.Hour)},
	} {
		key := []string{"rub-buy", "usd-buy", "sell"}[i]
		mutation := inv.Mutation{Action: "create", RequestKey: key, Operation: o, ExpectedLedgerVersion: version}
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
	// Read through a new service/repository, not the mutation result in memory.
	restarted := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	state, e := restarted.Snapshot(ctx, tenant, actor, ledger.ID, at.AddDate(0, 0, 1))
	if e != nil || len(state.Positions) != 1 || state.Positions[0].Quantity != "4" || state.Positions[0].CleanCost != "" || state.Positions[0].CostBasis != "principal" || len(state.Positions[0].Lots) != 2 {
		t.Fatal(state, e)
	}
	if state.Positions[0].Lots[0].Currency != "RUB" || state.Positions[0].Lots[0].CleanCost != "100" || state.Positions[0].Lots[1].Currency != "USD" {
		t.Fatal(state.Positions)
	}
	ops, e := restarted.Operations(ctx, tenant, actor, ledger.ID, 200, 0)
	if e != nil {
		t.Fatal(e)
	}
	feeCurrencies := map[string]bool{}
	for _, o := range ops {
		feeCurrencies[o.FeeCurrency] = true
	}
	if !feeCurrencies["USD"] || !feeCurrencies["RUB"] {
		t.Fatal("native fee currencies lost in JSON storage", ops)
	}
	// Explain the exact migration write and rollback predicate without ANALYZE.
	up, e := migrations.FS.ReadFile("0061_native_portfolio_lots.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	down, e := migrations.FS.ReadFile("0061_native_portfolio_lots.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	predicate := strings.Split(strings.Split(string(down), "IF EXISTS (")[1], ") THEN")[0]
	for _, query := range []string{"EXPLAIN " + string(up), "EXPLAIN SELECT EXISTS (" + predicate + ")"} {
		rows, e := pool.DB.Query(ctx, query)
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			var line string
			if e = rows.Scan(&line); e != nil {
				t.Fatal(e)
			}
			t.Log(line)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	if _, e = pool.DB.Exec(ctx, string(down)); e == nil {
		t.Fatal("downgrade accepted native history")
	}
	if _, e = restarted.Snapshot(ctx, tenant, actor, ledger.ID, at.AddDate(0, 0, 1)); e != nil {
		t.Fatal("failed downgrade altered data", e)
	}
}
