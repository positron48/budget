package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/positron48/budget/internal/adapter/postgres"
)

func TestQuoteInputRejectsPublicPermissionsUnknownFieldsAndTrailingData(t *testing.T) {
	for _, c := range []struct {
		name, body string
		mode       os.FileMode
	}{
		{"public", `[{}]`, 0644},
		{"unknown", `[{"unexpected":true}]`, 0600},
		{"trailing", `[{}] {}`, 0600},
		{"empty", `[]`, 0600},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "quotes.json")
			if e := os.WriteFile(path, []byte(c.body), c.mode); e != nil {
				t.Fatal(e)
			}
			if _, e := readQuotes(path); e == nil {
				t.Fatal("unsafe quote input accepted")
			}
		})
	}
}

func TestQuotePlanHasNoDatabaseAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quotes.json")
	data := []postgres.PortfolioQuoteSeed{{}}
	raw, e := json.Marshal(data)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	args, flags, output := os.Args, flag.CommandLine, os.Stdout
	defer func() { os.Args, flag.CommandLine, os.Stdout = args, flags, output }()
	flag.CommandLine = flag.NewFlagSet("portfolioquotes", flag.ContinueOnError)
	os.Args = []string{"portfolioquotes", "-input", path, "-plan"}
	t.Setenv("DATABASE_URL", "")
	reader, writer, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = reader.Close() }()
	os.Stdout = writer
	e = run()
	_ = writer.Close()
	if e != nil {
		t.Fatal(e)
	}
	raw, e = io.ReadAll(reader)
	if e != nil {
		t.Fatal(e)
	}
	var report struct {
		Status, SHA256               string
		SourceWrites, DatabaseAccess bool
		Quotes                       int
	}
	if json.Unmarshal(raw, &report) != nil || report.Status != "quote_plan" || report.SourceWrites || report.DatabaseAccess || report.Quotes != 1 || report.SHA256 != postgres.PortfolioQuotesSHA256(data) {
		t.Fatal("plan unexpectedly requires access or changes data")
	}
}
