package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"reflect"
	"testing"
	"time"
)

func TestInvestmentDistributionPersistence(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	if _, err := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Income','RUB')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Income", at.Add(-time.Hour), "income")
	if err != nil {
		t.Fatal(err)
	}

	version := int64(1)
	post := func(key string, o d.InvestmentOperation) d.InvestmentOperation {
		t.Helper()
		o.LedgerID = ledger.ID
		o.Currency = "RUB"
		posted, err := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: key, ExpectedLedgerVersion: version, Operation: o})
		if err != nil {
			t.Fatal(err)
		}
		version++
		return posted
	}
	post("deposit", d.InvestmentOperation{Kind: "deposit", Amount: "100", OccurredAt: at})
	buy := post("buy", d.InvestmentOperation{Kind: "buy", InstrumentKey: "OLD", Quantity: "2", Amount: "20", OccurredAt: at.Add(time.Hour)})
	action := post("action", d.InvestmentOperation{Kind: "distribution", InstrumentKey: "OLD", OccurredAt: at.Add(2 * time.Hour), Note: "Reviewed replacement", Distribution: &d.InvestmentDistribution{EventKey: "event", TargetInstrumentKey: "NEW", NewUnits: "7.0135", OldUnits: "1", CostNumerator: "1", CostDenominator: "1", ConsumeSource: true}})
	if action.Distribution == nil || action.Distribution.EventKey != "event" {
		t.Fatal(action)
	}
	snapshot, err := svc.Snapshot(ctx, tenant, actor, ledger.ID, at.Add(4*time.Hour))
	if err != nil || snapshot.Positions[0].Quantity != "14.027" || snapshot.Positions[0].CleanCost != "20" || snapshot.Positions[0].Lots[0].OriginID != buy.ID || snapshot.Positions[1].Quantity != "0" {
		t.Fatal(snapshot, err)
	}
	var legs int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_security_legs WHERE operation_id=$1`, action.ID).Scan(&legs); err != nil || legs != 2 {
		t.Fatal(legs, err)
	}
	duplicate := action
	duplicate.ID = ""
	if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "duplicate", ExpectedLedgerVersion: version, Operation: duplicate}); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal(err)
	}
	var count int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE ledger_id=$1`, ledger.ID).Scan(&count); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	sale := post("sale", d.InvestmentOperation{Kind: "sell", InstrumentKey: "NEW", Quantity: "14", Amount: "30", OccurredAt: at.Add(3 * time.Hour)})
	before, err := svc.Snapshot(ctx, tenant, actor, ledger.ID, at.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	cancelAction := inv.Mutation{Action: "void", RequestKey: "cancel-action", ExpectedLedgerVersion: version, ExpectedOperationVersion: action.Version, Reason: "Correct corporate event", Operation: action}
	if _, err = svc.Mutate(ctx, tenant, actor, cancelAction); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("dependent sale should prevent void", err)
	}
	// A correction that shrinks the entitlement below the sold quantity is also
	// rejected before replacing either instrument's persisted legs.
	changed := action
	terms := *action.Distribution
	terms.NewUnits = "1"
	changed.Distribution = &terms
	if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "amend", RequestKey: "bad-ratio", ExpectedLedgerVersion: version, ExpectedOperationVersion: action.Version, Reason: "Correct ratio", Operation: changed}); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal(err)
	}
	after, err := svc.Snapshot(ctx, tenant, actor, ledger.ID, at.Add(4*time.Hour))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rejected mutation changed snapshot", before, after, err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_security_legs WHERE operation_id=$1`, action.ID).Scan(&legs); err != nil || legs != 2 {
		t.Fatal(legs, err)
	}
	// Cancel the dependent sale first, then retry the previously rejected request
	// with the current ledger version. Both instruments return to their old state.
	if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "void", RequestKey: "cancel-sale", ExpectedLedgerVersion: version, ExpectedOperationVersion: sale.Version, Reason: "Cancel dependent sale", Operation: sale}); err != nil {
		t.Fatal(err)
	}
	version++
	cancelAction.ExpectedLedgerVersion = version
	cancelled, err := svc.Mutate(ctx, tenant, actor, cancelAction)
	if err != nil || cancelled.Status != "voided" {
		t.Fatal(cancelled, err)
	}
	restored, err := svc.Snapshot(ctx, tenant, actor, ledger.ID, at.Add(4*time.Hour))
	if err != nil || len(restored.Positions) != 1 || restored.Positions[0].InstrumentKey != "OLD" || restored.Positions[0].Quantity != "2" || restored.Positions[0].CleanCost != "20" || restored.Cash[0].Economic != "80" {
		t.Fatal(restored, err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_security_legs WHERE operation_id=$1`, action.ID).Scan(&legs); err != nil || legs != 0 {
		t.Fatal(legs, err)
	}

}
