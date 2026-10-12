package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	av "github.com/positron48/budget/internal/adapter/alphavantage"
)

func fixtureAudit(pair string) av.Audit {
	return av.Audit{Source: "alphavantage", Pair: pair, From: "2019-10-01", To: "2026-10-11", Timezone: "UTC", LastRefreshed: "2026-10-12 00:00:00", BodySHA256: strings.Repeat("a", 64), RetrievedAt: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC), Bars: []av.Bar{{Date: "2026-10-09", Open: "80", High: "82", Low: "79", Close: "81"}}}
}
func TestResumeAndSummaryUseSavedEvidenceWithoutKeychain(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "audit")
	m := manifest{"alphavantage", "2019-10-01", "2026-10-11"}
	if err := prepare(dir, m); err != nil {
		t.Fatal(err)
	}
	for _, pair := range pairs {
		if err := publish(filepath.Join(dir, strings.ReplaceAll(pair, "/", "-")+".json"), fixtureAudit(pair)); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"-out", dir, "-from", m.From, "-to", m.To, "-keychain-account", "nonexistent-fixture-account"}
	if err := run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), append(args, "-summary")); err != nil {
		t.Fatal(err)
	}
	if err := prepare(dir, manifest{"alphavantage", "2020-01-01", m.To}); err == nil {
		t.Fatal("range mismatch accepted")
	}
}
func TestAtomicCheckpointDoesNotOverwriteAndRejectsTrailingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	a := fixtureAudit("USD/RUB")
	if err := publish(path, a); err != nil {
		t.Fatal(err)
	}
	if err := publish(path, a); err == nil {
		t.Fatal("overwritten")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unsafe permissions")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(raw, []byte("{}")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err = load(path, &a); err == nil {
		t.Fatal("trailing data accepted")
	}
}
func TestPrivateEvidencePaths(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0755); err != nil {
		t.Fatal(err)
	}
	if err := prepare(shared, manifest{}); err == nil {
		t.Fatal("shared directory accepted")
	}
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(shared, linked); err != nil {
		t.Fatal(err)
	}
	if err := prepare(linked, manifest{}); err == nil {
		t.Fatal("symlink accepted")
	}
	path := filepath.Join(root, "shared.json")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	var value any
	if err := load(path, &value); err == nil {
		t.Fatal("shared checkpoint accepted")
	}
}
func TestInvalidWindowDoesNotCreateEvidence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new")
	if err := run(context.Background(), []string{"-out", dir, "-from", "invalid", "-to", "2026-10-11"}); err == nil {
		t.Fatal("invalid window accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid window created files")
	}
}
