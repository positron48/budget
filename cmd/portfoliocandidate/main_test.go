package main

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestCandidateDatabaseCannotBeOriginalOrInjectedName(t *testing.T) {
	for _, name := range []string{"budget", "postgres", "budget_portfolio_candidate_", "budget_portfolio_candidate_x;DROP DATABASE budget"} {
		if _, e := candidateURL("postgres://user:secret@localhost/budget", name); e == nil {
			t.Fatal("unsafe database name accepted")
		}
	}
	dsn, e := candidateURL("postgres://user:p%40ss@localhost:5432/budget?sslmode=disable", "budget_portfolio_candidate_test")
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := url.Parse(dsn)
	if e != nil || parsed.Path != "/budget_portfolio_candidate_test" || parsed.Query().Get("sslmode") != "disable" {
		t.Fatal("candidate connection scope changed", e)
	}
	if _, e = candidateURL(dsn, "budget_portfolio_candidate_test"); e == nil {
		t.Fatal("same database accepted")
	}
}
func TestCandidateFilesArePrivateAndNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.json")
	if e := privateWrite(path, map[string]string{"value": "original"}); e != nil {
		t.Fatal(e)
	}
	if e := privateWrite(path, map[string]string{"value": "replacement"}); e == nil {
		t.Fatal("overwritten candidate")
	}
	var value map[string]string
	if e := privateRead(path, &value); e != nil || value["value"] != "original" {
		t.Fatal(value, e)
	}
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if e := privateRead(path, &value); e == nil {
		t.Fatal("public candidate file accepted")
	}
}
