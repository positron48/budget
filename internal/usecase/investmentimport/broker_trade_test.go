package investmentimport

import (
	"encoding/json"
	"testing"
)

func brokerTradeFixture(t *testing.T, modify func(map[string]any, map[string]any)) BrokerSnapshot {
	t.Helper()
	money := func(units string, nano int64) map[string]any {
		return map[string]any{"units": units, "nano": nano, "currency": "rub"}
	}
	trade := map[string]any{"id": "trade", "brokerAccountId": "a", "state": "OPERATION_STATE_EXECUTED", "type": "OPERATION_TYPE_BUY", "instrumentType": "share", "instrumentUid": "11111111-1111-4111-8111-111111111111", "date": "2024-03-01T00:00:00Z", "quantityDone": "2", "payment": money("-20", -120000000), "commission": money("0", -300000000), "tradesInfo": map[string]any{"trades": []map[string]any{{"num": "fill", "date": "2024-03-01T00:00:01Z", "quantity": "2"}}}}
	fee := map[string]any{"id": "fee", "brokerAccountId": "a", "state": "OPERATION_STATE_EXECUTED", "type": "OPERATION_TYPE_BROKER_FEE", "parentOperationId": "trade", "date": "2024-03-01T00:00:02Z", "payment": money("0", -300000000)}
	if modify != nil {
		modify(trade, fee)
	}
	rows, _ := json.Marshal([]map[string]any{trade, fee})
	snapshot, err := PrepareBrokerSnapshot(brokerFixture(t, string(rows)))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func TestBrokerTradeActualCashAndFeeSources(t *testing.T) {
	s := brokerTradeFixture(t, nil)
	c, err := PrepareBrokerTrade(s, "trade", "22222222-2222-4222-8222-222222222222")
	if err != nil || c.Operation.Amount != "20.12" || c.Operation.Fee != "0.3" || !c.Operation.SettlementAssumed || len(c.SourceIdentities) != 2 || c.SourceIdentities[1] != "tinvest:a:fee" {
		t.Fatal(c, err)
	}
	if c.Operation.OccurredAt.Second() != 1 || c.SnapshotSHA256 != s.SHA256 {
		t.Fatal(c)
	}
}
func TestBrokerTradeRejectsIncompleteFacts(t *testing.T) {
	for _, modify := range []func(map[string]any, map[string]any){
		func(a, b map[string]any) { delete(a, "commission") },
		func(a, b map[string]any) { a["quantityDone"] = "3" },
		func(a, b map[string]any) { a["state"] = "OPERATION_STATE_CANCELED" },
		func(a, b map[string]any) { a["instrumentType"] = "bond" },
		func(a, b map[string]any) { b["parentOperationId"] = "unrelated" },
		func(a, b map[string]any) { b["payment"] = map[string]any{"currency": "rub", "units": "-1", "nano": 0} },
		func(a, b map[string]any) { delete(a, "tradesInfo") },
	} {
		c, err := PrepareBrokerTrade(brokerTradeFixture(t, modify), "trade", "22222222-2222-4222-8222-222222222222")
		if err == nil || c.Operation.Kind != "" || len(c.SourceIdentities) != 0 {
			t.Fatal(c, err)
		}
	}
}
