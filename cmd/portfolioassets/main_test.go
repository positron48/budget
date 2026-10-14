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

func TestLinkInputRejectsPublicPermissionsUnknownFieldsAndTrailingData(t *testing.T) {
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
			path := filepath.Join(t.TempDir(), "links.json")
			if e := os.WriteFile(path, []byte(c.body), c.mode); e != nil {
				t.Fatal(e)
			}
			if _, e := readLinks(path); e == nil {
				t.Fatal("unsafe link input accepted")
			}
		})
	}
}

func TestLinkPlanHasNoDatabaseAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.json")
	data := []postgres.TrackerAssetLink{{}}
	raw, e := json.Marshal(data)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	args, flags, output := os.Args, flag.CommandLine, os.Stdout
	defer func() { os.Args, flag.CommandLine, os.Stdout = args, flags, output }()
	flag.CommandLine = flag.NewFlagSet("portfoliolinks", flag.ContinueOnError)
	os.Args = []string{"portfoliolinks", "-input", path, "-plan"}
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
		Links                        int
	}
	if json.Unmarshal(raw, &report) != nil || report.Status != "link_plan" || report.SourceWrites || report.DatabaseAccess || report.Links != 1 || report.SHA256 != postgres.TrackerAssetLinksSHA256(data) {
		t.Fatal("plan unexpectedly requires access or changes data")
	}
}
