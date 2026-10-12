package main

import (
	"context"
	"fmt"
	td "github.com/positron48/budget/internal/adapter/twelvedata"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckpointPublicationNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "evidence.json")
	if e := publish(p, map[string]int{"value": 1}); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(p)
	if e := publish(p, map[string]int{"value": 2}); e == nil {
		t.Fatal("overwritten")
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("changed evidence")
	}
	i, _ := os.Stat(p)
	if i.Mode().Perm() != 0600 {
		t.Fatal(i.Mode())
	}
}
func TestOfflineSummaryDoesNotRequireCredentials(t *testing.T) {
	dir := t.TempDir()
	m := manifest{Source: "twelvedata", From: "2024-06-10", To: "2024-06-28"}
	if e := prepare(dir, m); e != nil {
		t.Fatal(e)
	}
	if e := run(context.Background(), []string{"-out", dir, "-from", m.From, "-to", m.To, "-summary", "-keychain-account", "not-a-real-account"}); e != nil {
		t.Fatal(e)
	}
	if e := prepare(dir, manifest{Source: m.Source, From: m.From, To: "2024-06-29"}); e == nil {
		t.Fatal("changed range accepted")
	}
}
func TestCheckpointIdentityRejectedOffline(t *testing.T) {
	dir := t.TempDir()
	m := manifest{Source: "twelvedata", From: "2024-06-10", To: "2024-06-28"}
	if e := prepare(dir, m); e != nil {
		t.Fatal(e)
	}
	if e := publish(filepath.Join(dir, "USD-RUB.json"), map[string]any{"source": "twelvedata", "meta": map[string]string{"symbol": "EUR/RUB"}, "from": m.From, "to": m.To}); e != nil {
		t.Fatal(e)
	}
	if e := run(context.Background(), []string{"-out", dir, "-from", m.From, "-to", m.To, "-summary"}); e == nil {
		t.Fatal("mismatched checkpoint accepted")
	}
}

func TestCompletedAuditResumesWithoutKeychain(t *testing.T) {
	dir := t.TempDir()
	m := manifest{Source: "twelvedata", From: "2024-06-10", To: "2024-06-28"}
	if err := prepare(dir, m); err != nil {
		t.Fatal(err)
	}
	for _, pair := range pairs {
		raw := []byte(fmt.Sprintf(`{"status":"ok","meta":{"symbol":%q,"interval":"1day","type":"Physical Currency","exchange_timezone":"Australia/Sydney"},"values":[{"datetime":"2024-06-12","open":"90","high":"91","low":"89","close":"90.12345678901"}]}`, pair))
		audit, err := td.Parse(raw, pair, m.From, m.To, time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if err := publish(filepath.Join(dir, strings.ReplaceAll(pair, "/", "-")+".json"), audit); err != nil {
			t.Fatal(err)
		}
	}
	if err := run(context.Background(), []string{"-out", dir, "-from", m.From, "-to", m.To, "-keychain-account", "nonexistent"}); err != nil {
		t.Fatal(err)
	}
}
func TestTrailingEvidenceIsRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(p, []byte(`{"Source":"twelvedata"} {"Source":"other"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := load(p, &m); err == nil {
		t.Fatal("ambiguous evidence accepted")
	}
}
