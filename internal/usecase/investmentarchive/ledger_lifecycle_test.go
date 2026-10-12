package investmentarchive

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLedgerMetadataArchiveChainAndTimezone(t *testing.T) {
	const ledger = "33333333-3333-4333-8333-333333333333"
	const actor = "22222222-2222-4222-8222-222222222222"
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	first := storedLedger{ID: ledger, Tenant: tenant, Name: "IIS", Started: at, Created: at, Version: 1, Broker: "Broker", AccountType: "iis", DefaultCurrency: "USD", MetadataVersion: 1}
	second := first
	second.Name = "Regular"
	second.AccountType = "regular"
	second.MetadataVersion = 2
	second.Version = 3
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	fixture := func() Dataset {
		head := second
		head.Version = 7
		// PostgreSQL row JSON uses numeric offsets; audit payloads often use Z.
		h := json.RawMessage(strings.ReplaceAll(string(raw(head)), "00:00:00Z", "00:00:00+00:00"))
		return Dataset{TenantID: tenant, Actors: []string{actor}, Tables: []Table{{Name: "investment_ledgers", Rows: []json.RawMessage{h}}, {Name: "investment_ledger_revisions", Rows: []json.RawMessage{raw(map[string]any{"tenant_id": tenant, "ledger_id": ledger, "version": 1, "action": "create", "actor_id": actor, "after_data": first}), raw(map[string]any{"tenant_id": tenant, "ledger_id": ledger, "version": 2, "action": "edit", "actor_id": actor, "before_data": first, "after_data": second})}}}}
	}
	if e := VerifyLedgerLifecycle(fixture()); e != nil {
		t.Fatal("equivalent timestamps/financial revisions", e)
	}
	for _, change := range []func(map[string]any){func(v map[string]any) { v["before_data"].(map[string]any)["broker"] = "Changed past" }, func(v map[string]any) { v["action"] = "archive" }, func(v map[string]any) { v["actor_id"] = "unknown" }, func(v map[string]any) { v["after_data"].(map[string]any)["default_currency"] = "EUR" }} {
		data := fixture()
		var v map[string]any
		if e := json.Unmarshal(data.Tables[1].Rows[1], &v); e != nil {
			t.Fatal(e)
		}
		change(v)
		data.Tables[1].Rows[1] = raw(v)
		if VerifyLedgerLifecycle(data) == nil {
			t.Fatal("corrupt metadata accepted")
		}
	}
}
