package main

import (
	"encoding/json"
	"flag"
	pg "github.com/positron48/budget/internal/adapter/postgres"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveFileIsPrivateAndNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.json")
	if e := writeArchive(path, archive.Bundle{Format: archive.Format, SHA256: "fixture"}); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("non-private archive permissions", e)
	}
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = writeArchive(path, archive.Bundle{}); e == nil {
		t.Fatal("overwrote existing archive")
	}
	after, e := os.ReadFile(path)
	if e != nil || string(before) != string(after) {
		t.Fatal("changed existing archive", e)
	}
}

func TestRestoreModesRequireExplicitChecksum(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"-market-file", "market.json"}, "required together"},
		{[]string{"-expected-market-sha256", "sha"}, "required together"},
		{[]string{"-market-file", "market.json", "-expected-market-sha256", "sha"}, "requires replacement"},
		{[]string{"-scope-ledger", "not-uuid"}, "canonical scope ledger"},
		{[]string{"-scope-ledger", "ledger", "-restore", "file"}, "scope-ledger cannot"},
		{[]string{"-replace", "archive.json", "-actor", "actor", "-expected-sha", "sha"}, "replacement requires"},
		{[]string{"-check-replace", "archive.json", "-actor", "actor"}, "replacement requires"},
		{[]string{"-before-state-sha256", "sha"}, "requires replacement"},
		{[]string{"-replace", "a", "-check-replace", "a"}, "replacement requires"},
		{[]string{"-restore", "archive.json", "-actor", "actor"}, "restore requires"},
		{[]string{"-restore", "archive.json", "-actor", "actor", "-expected-sha", "sha", "-verify", "archive.json"}, "restore requires"},
		{[]string{"-expected-sha", "sha"}, "expected-sha requires"},
		{[]string{"-check-restore", "archive.json"}, "check-restore requires"},
		{[]string{"-check-restore", "archive.json", "-actor", "actor", "-output", "out.json"}, "check-restore requires"},
	} {
		func() {
			savedArgs, savedFlags := os.Args, flag.CommandLine
			defer func() { os.Args, flag.CommandLine = savedArgs, savedFlags }()
			os.Args = append([]string{"investmentarchive"}, test.args...)
			flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
			if err := run(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("%v: %v", test.args, err)
			}
		}()
	}
}

func TestReplacementMarketRequiresPrivateExactEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market.json")
	input := pg.PortfolioMarketInput{}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	sha := pg.PortfolioMarketSHA256(input)
	if _, err = readReplacementMarket(path, sha); err != nil {
		t.Fatal(err)
	}
	if _, err = readReplacementMarket(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("unreviewed evidence accepted")
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = readReplacementMarket(path, sha); err == nil {
		t.Fatal("public evidence accepted")
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(raw, []byte(" {}")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readReplacementMarket(path, sha); err == nil {
		t.Fatal("trailing data accepted")
	}
}
