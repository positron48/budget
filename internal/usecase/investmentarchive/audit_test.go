package investmentarchive

import (
	"encoding/json"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

func auditFixture() Dataset {
	const actor = "22222222-2222-4222-8222-222222222222"
	const ledger = "33333333-3333-4333-8333-333333333333"
	const operation = "44444444-4444-4444-8444-444444444444"
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	first := d.InvestmentOperation{ID: operation, TenantID: tenant, LedgerID: ledger, ActorID: actor, Sequence: 1, Version: 1, Kind: "deposit", Status: "posted", Currency: "RUB", Amount: "100", OccurredAt: at, SettlementAt: at, CreatedAt: at}
	second := first
	second.Version, second.Amount = 2, "200"
	third := second
	third.Version, third.Status = 3, "voided"
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	return Dataset{TenantID: tenant, Actors: []string{actor}, Tables: []Table{
		{Name: "investment_ledgers", Rows: []json.RawMessage{raw(map[string]any{"id": ledger, "version": 4, "next_sequence": 2})}},
		{Name: "investment_operations", Rows: []json.RawMessage{raw(storedOperation{ID: operation, Data: third})}},
		{Name: "investment_operation_revisions", Rows: []json.RawMessage{
			raw(operationRevision{Tenant: tenant, Ledger: ledger, Operation: operation, Version: 1, Action: "create", Actor: actor, After: first}),
			raw(operationRevision{Tenant: tenant, Ledger: ledger, Operation: operation, Version: 2, Action: "amend", Actor: actor, Before: &first, After: second}),
			raw(operationRevision{Tenant: tenant, Ledger: ledger, Operation: operation, Version: 3, Action: "void", Actor: actor, Before: &second, After: third}),
		}},
	}}
}
func TestJournalAuditChain(t *testing.T) {
	if err := VerifyJournalAudit(auditFixture()); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*operationRevision)
	}{
		{"gap", func(r *operationRevision) { r.Version = 4 }},
		{"before", func(r *operationRevision) { r.Before.Amount = "50" }},
		{"created", func(r *operationRevision) { r.After.CreatedAt = r.After.CreatedAt.Add(time.Second) }},
		{"identity", func(r *operationRevision) { r.After.LedgerID = tenant }},
		{"actor", func(r *operationRevision) { r.After.ActorID = tenant }},
		{"source", func(r *operationRevision) { r.After.Source = &d.InvestmentSource{ImportID: tenant} }},
		{"void_amount", func(r *operationRevision) { r.After.Amount = "300" }},
		{"void_kind", func(r *operationRevision) { r.After.Kind = "withdrawal" }},
		{"illegal_action", func(r *operationRevision) { r.Action = "create" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := auditFixture()
			var r operationRevision
			if err := json.Unmarshal(data.Tables[2].Rows[2], &r); err != nil {
				t.Fatal(err)
			}
			test.change(&r)
			data.Tables[2].Rows[2], _ = json.Marshal(r)
			if err := VerifyJournalAudit(data); err == nil {
				t.Fatal("corrupted chain accepted")
			}
		})
	}
}
