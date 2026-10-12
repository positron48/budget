package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	av "github.com/positron48/budget/internal/adapter/alphavantage"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func checkpointFixture(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	policy := md.FXPolicy{Key: "alpha-retrospective", Mode: "market", Source: "alphavantage", AccessScope: "personal", SourceReference: "Synthetic fixture", TimeConvention: md.AlphaTimeConvention, ValueConvention: md.AlphaValueConvention, ValidityConvention: md.AlphaValidityConvention, Alpha: &md.AlphaFXPolicy{From: day, Convention: md.AlphaRetrospectiveConvention}}
	write := func(name string, value any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("policy.json", document{Policy: policy, Version: 1})
	write("manifest.json", struct{ Source, From, To string }{"alphavantage", "2024-01-05", "2024-01-07"})
	for _, pair := range []string{"USD/RUB", "EUR/RUB"} {
		write(strings.ReplaceAll(pair, "/", "-")+".json", av.Audit{Source: "alphavantage", Pair: pair, From: "2024-01-05", To: "2024-01-07", Timezone: "UTC", LastRefreshed: "2024-01-05", BodySHA256: strings.Repeat("a", 64), RetrievedAt: day.Add(96 * time.Hour), Bars: []av.Bar{{Date: "2024-01-05", Open: "90", High: "100", Low: "80", Close: "90"}}})
	}
	return dir, filepath.Join(dir, "policy.json"), "11111111-1111-4111-8111-111111111111"
}
func TestPlanBindsTenantPolicyAndCompleteCheckpoints(t *testing.T) {
	dir, file, tenant := checkpointFixture(t)
	_, hash, n, err := loadPlan(dir, file, tenant)
	if err != nil || n != 2 || len(hash) != 64 {
		t.Fatal(hash, n, err)
	}
	_, other, _, err := loadPlan(dir, file, "22222222-2222-4222-8222-222222222222")
	if err != nil || hash == other {
		t.Fatal("tenant absent from digest", err)
	}
	t.Setenv("DATABASE_URL", "")
	if err = run([]string{"-checkpoints", dir, "-policy-file", file, "-tenant", tenant}); err != nil {
		t.Fatal("offline preview", err)
	}
	if err = run([]string{"-checkpoints", dir, "-policy-file", file, "-tenant", tenant, "-apply", "-plan-sha256", "wrong"}); err == nil || !strings.Contains(err.Error(), "plan changed") {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(dir, "EUR-RUB.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = loadPlan(dir, file, tenant); err == nil {
		t.Fatal("partial history accepted")
	}
}
func TestPlanRejectsUnsafeEvidence(t *testing.T) {
	dir, file, tenant := checkpointFixture(t)
	path := filepath.Join(dir, "USD-RUB.json")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadPlan(dir, file, tenant); err == nil {
		t.Fatal("public checkpoint accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(raw, []byte(" {}")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = loadPlan(dir, file, tenant); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
