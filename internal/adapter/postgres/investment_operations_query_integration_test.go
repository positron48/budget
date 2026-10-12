package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"testing"
	"time"
)

func TestInvestmentOperationsQueryAndRevisionHistory(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, other, user, outsider string
	for _, f := range []struct {
		sql string
		out *string
	}{{`INSERT INTO tenants(name,default_currency_code) VALUES('Operations','USD') RETURNING id`, &tenant}, {`INSERT INTO tenants(name,default_currency_code) VALUES('Other','USD') RETURNING id`, &other}, {fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('operations-%d@example.test','fixture') RETURNING id`, time.Now().UnixNano()), &user}, {fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('outsider-op-%d@example.test','fixture') RETURNING id`, time.Now().UnixNano()), &outsider}} {
		if e := pool.DB.QueryRow(ctx, f.sql).Scan(f.out); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner'),($3,$2,'owner')`, tenant, user, other); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Date(2024, 1, 31, 22, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, user, "Broker", at.AddDate(0, -1, 0), "ledger")
	if e != nil {
		t.Fatal(e)
	}
	second, e := svc.CreateLedger(ctx, tenant, user, "Second", at, "second")
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := svc.CreateLedger(ctx, other, user, "Other", at, "foreign")
	if e != nil {
		t.Fatal(e)
	}
	version := int64(1)
	create := func(kind, amount, key string) d.InvestmentOperation {
		t.Helper()
		o := d.InvestmentOperation{LedgerID: ledger.ID, Kind: kind, Amount: amount, Currency: "USD", OccurredAt: at}
		v, e := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: key, ExpectedLedgerVersion: version, Operation: o})
		if e != nil {
			t.Fatal(e)
		}
		version++
		return v
	}
	create("deposit", "1000", "deposit")
	fee := create("fee", "0.123456789012", "fee")
	create("tax_refund", "0.000000000001", "refund")
	q := inv.OperationsQuery{FromDate: "2024-02-01", ToDate: "2024-02-29", Limit: 1}
	first, e := svc.QueryOperations(ctx, tenant, user, q)
	if e != nil || first.TotalCount != 3 || len(first.Entries) != 1 || first.NextCursor == "" {
		t.Fatal(e, first)
	}
	seen := map[string]bool{}
	page := first
	for {
		for _, r := range page.Entries {
			if seen[r.Operation.ID] {
				t.Fatal("repeated page")
			}
			seen[r.Operation.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
		page, e = svc.QueryOperations(ctx, tenant, user, q)
		if e != nil {
			t.Fatal(e)
		}
	}
	hq := inv.OperationHistoryQuery{LedgerID: ledger.ID, OperationID: fee.ID, ExpectedVersion: 1, Limit: 1}
	history, e := svc.OperationHistory(ctx, tenant, user, hq)
	if e != nil || len(history.Revisions) != 1 || history.Revisions[0].Before != nil || history.NextAfterVersion != 0 {
		t.Fatal(e, history)
	}
	fee.Amount = "0.223456789012"
	fee, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "amend", RequestKey: "amend", Reason: "Statement correction", ExpectedLedgerVersion: version, ExpectedOperationVersion: fee.Version, Operation: fee})
	if e != nil {
		t.Fatal(e)
	}
	version++
	if _, e = svc.OperationHistory(ctx, tenant, user, hq); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale audit head", e)
	}
	q.Cursor = first.NextCursor
	if _, e = svc.QueryOperations(ctx, tenant, user, q); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale list", e)
	}
	hq.ExpectedVersion = fee.Version
	history, e = svc.OperationHistory(ctx, tenant, user, hq)
	if e != nil || history.NextAfterVersion != 1 || history.Current.Amount != "0.223456789012" {
		t.Fatal(e, history)
	}
	hq.AfterVersion = history.NextAfterVersion
	history, e = svc.OperationHistory(ctx, tenant, user, hq)
	if e != nil || history.Revisions[0].Before.Amount != "0.123456789012" || history.Revisions[0].After.Amount != "0.223456789012" || history.Revisions[0].Reason != "Statement correction" || history.Revisions[0].ActorID != user {
		t.Fatal("audit terms", e, history)
	}
	fee, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "void", RequestKey: "void", Reason: "Duplicate fee", ExpectedLedgerVersion: version, ExpectedOperationVersion: fee.Version, Operation: fee})
	if e != nil {
		t.Fatal(e)
	}
	version++
	hq.ExpectedVersion = fee.Version
	hq.AfterVersion = 0
	hq.Limit = 200
	history, e = svc.OperationHistory(ctx, tenant, user, hq)
	if e != nil || len(history.Revisions) != 3 || history.Revisions[2].Action != "void" || history.Current.Status != "voided" {
		t.Fatal(e, history)
	}
	q.Cursor = ""
	q.Status = "voided"
	q.Kind = "fee"
	q.Origin = "manual"
	page, e = svc.QueryOperations(ctx, tenant, user, q)
	if e != nil || page.TotalCount != 1 || len(page.Entries[0].Cash) != 0 {
		t.Fatal("voided terms/cash", e, page)
	}
	q.LedgerIDs = []string{second.ID}
	page, e = svc.QueryOperations(ctx, tenant, user, q)
	if e != nil || page.TotalCount != 0 {
		t.Fatal("scope", e)
	}
	q.LedgerIDs = []string{foreign.ID}
	if _, e = svc.QueryOperations(ctx, tenant, user, q); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign list", e)
	}
	hq.LedgerID = foreign.ID
	if _, e = svc.OperationHistory(ctx, tenant, user, hq); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign audit", e)
	}
	hq.LedgerID = ledger.ID
	if _, e = repo.OperationHistory(ctx, tenant, outsider, hq); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("membership bypass", e)
	}
	// Inspect the exact read shape; this small synthetic plan is not a production latency claim.
	var explain json.RawMessage
	if e = pool.DB.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+queryInvestmentOperationsSQL, tenant, []string{ledger.ID}, inv.MaxJournalQueryOperations+1).Scan(&explain); e != nil || !json.Valid(explain) {
		t.Fatal("query plan", e)
	}
	// Tampering with the before/after chain must not be presented as valid history.
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_operation_revisions SET after_data=jsonb_set(after_data,'{Amount}','"9"'::jsonb) WHERE tenant_id=$1 AND operation_id=$2 AND version=1`, tenant, fee.ID); e != nil {
		t.Fatal(e)
	}
	hq.AfterVersion = 1
	hq.Limit = 1
	if _, e = svc.OperationHistory(ctx, tenant, user, hq); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("corrupt audit boundary accepted", e)
	}
	// A JSON head disagreeing with indexed columns must fail closed in both reads.
	if _, e = pool.DB.Exec(ctx, `UPDATE investment_operations SET data=jsonb_set(data,'{Version}','999'::jsonb) WHERE tenant_id=$1 AND id=$2`, tenant, fee.ID); e != nil {
		t.Fatal(e)
	}
	q.LedgerIDs = []string{ledger.ID}
	if _, e = svc.QueryOperations(ctx, tenant, user, q); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("corrupt operation head accepted by list", e)
	}
	if _, e = svc.OperationHistory(ctx, tenant, user, hq); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("corrupt operation head accepted by history", e)
	}
}
