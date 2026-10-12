package postgres

import (
	"encoding/json"
	"testing"
)

func TestArchiveRowComparisonKeepsExactValues(t *testing.T) {
	a := []json.RawMessage{json.RawMessage(`{"value":9007199254740993.123456789012,"note":"<>&","nested":{"b":2,"a":1}}`)}
	b := []json.RawMessage{json.RawMessage(`{ "nested":{"a":1,"b":2}, "note":"\u003c\u003e\u0026", "value":9007199254740993.123456789012 }`)}
	equal, e := archiveRowsEqual(a, b)
	if e != nil || !equal {
		t.Fatal("encoding-only differences rejected", e)
	}
	b[0] = json.RawMessage(`{"value":9007199254740993.123456789013,"note":"<>&","nested":{"b":2,"a":1}}`)
	equal, e = archiveRowsEqual(a, b)
	if e != nil || equal {
		t.Fatal("decimal difference lost", e)
	}
	b[0] = json.RawMessage(`{"value":9007199254740992.123456789012,"note":"<>&","nested":{"b":2,"a":1}}`)
	equal, e = archiveRowsEqual(a, b)
	if e != nil || equal {
		t.Fatal("integer difference above JS precision lost", e)
	}
}
