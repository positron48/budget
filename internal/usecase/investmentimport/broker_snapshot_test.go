package investmentimport

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func brokerFixture(t *testing.T, operations string) []byte {
	t.Helper()
	var ops []json.RawMessage
	if err := json.Unmarshal([]byte(operations), &ops); err != nil {
		t.Fatal(err)
	}
	compact, _ := json.Marshal(ops)
	return []byte(`{"from":"2024-01-01T00:00:00Z","to":"2025-01-01T00:00:00Z","account":{"id":"a","type":"ACCOUNT_TYPE_TINKOFF","openedDate":"2024-01-01T00:00:00Z"},"operations":` + operations + `,"history_evidence":{"version":1,"complete":true,"pages":[{"cursor":"","next_cursor":"","has_next":false,"count":` + strconv.Itoa(len(ops)) + `,"items_sha256":"` + brokerHash(compact) + `"}]}}`)
}

const sourceBrokerOp = `{"id":"one","brokerAccountId":"a","state":"OPERATION_STATE_CANCELED","type":"FUTURE_TYPE","date":"2024-03-01T00:00:00Z","quantityDone":"9007199254740993","extra":{"keep":true}}`

func TestBrokerSnapshotPreservesUnknownAndCancelledFacts(t *testing.T) {
	raw := brokerFixture(t, "["+sourceBrokerOp+"]")
	result, err := PrepareBrokerSnapshot(raw)
	if err != nil || len(result.Rows) != 1 || result.Rows[0].Identity != "tinvest:a:one" || !result.StartsAtAccountOpening || string(result.Original) != string(raw) {
		t.Fatal(result, err)
	}
	if !strings.Contains(string(result.Rows[0].Raw), "9007199254740993") || !strings.Contains(string(result.Rows[0].Raw), "FUTURE_TYPE") {
		t.Fatal(result.Rows)
	}
	raw[0] = '!'
	if result.Original[0] != '{' {
		t.Fatal("aliased input")
	}
}
func TestBrokerSnapshotRejectsTamperingAndWrongScope(t *testing.T) {
	valid := string(brokerFixture(t, "["+sourceBrokerOp+"]"))
	for _, raw := range []string{strings.Replace(valid, `"complete":true`, `"complete":false`, 1), strings.Replace(valid, `"count":1`, `"count":0`, 1), strings.Replace(valid, "9007199254740993", "1", 1), string(brokerFixture(t, "["+strings.Replace(sourceBrokerOp, `"brokerAccountId":"a"`, `"brokerAccountId":"b"`, 1)+"]")), string(brokerFixture(t, "["+sourceBrokerOp+","+sourceBrokerOp+"]")), string(brokerFixture(t, "["+strings.Replace(sourceBrokerOp, "2024-03-01", "2023-03-01", 1)+"]"))} {
		if _, err := PrepareBrokerSnapshot([]byte(raw)); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
}
