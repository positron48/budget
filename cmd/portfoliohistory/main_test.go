package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/positron48/budget/internal/adapter/postgres"
)

func TestHistoryInputIsPrivateBoundedAndChecksummed(t *testing.T) {
	batch := postgres.HistoricalPriceBatch{Version: 1}
	raw, e := json.Marshal(batch)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "input.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	sha := postgres.HistoricalPricesSHA256(batch)
	if got, e := readInput(path, sha); e != nil || got.Version != 1 {
		t.Fatal(got, e)
	}
	if _, e = readInput(path, "wrong"); e == nil {
		t.Fatal("checksum mismatch accepted")
	}
	if e = os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = readInput(path, sha); e == nil {
		t.Fatal("public history input accepted")
	}
	if e = os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
	for _, invalid := range []string{string(raw) + " {}", `{"Version":1,"unknown":true}`, "not-json"} {
		if e = os.WriteFile(path, []byte(invalid), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = readInput(path, sha); e == nil {
			t.Fatal("malformed input accepted")
		}
	}
}
