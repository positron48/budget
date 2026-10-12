package main

import (
	"encoding/json"
	"flag"
	"github.com/google/uuid"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinancialPreviewPrivateAndExclusive(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "source.csv")
	mapping := filepath.Join(dir, "mapping.json")
	output := filepath.Join(dir, "review.json")
	resolution := filepath.Join(dir, "resolution.json")
	data := "Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nCASH_IN,2024-01-01 12:00:00,,100,1,RUB,0,,0,,False,\n"
	if e := os.WriteFile(csv, []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
	preview, e := imp.PreviewSnowball(strings.NewReader(data), true)
	if e != nil {
		t.Fatal(e)
	}
	config := imp.FinancialMapping{SourceSHA256: preview.SourceSHA256, Timezone: "UTC", SettlementPolicy: "assume_trade_time", LedgerByRecord: map[int]string{1: uuid.NewString()}, Rules: []imp.FinancialRule{{Event: "CASH_IN", Currency: "RUB", Kind: "deposit", AmountBasis: "price", AmountFactor: "1", FeePolicy: "zero_only", Reason: "Confirmed statement cash"}}}
	raw, e := json.Marshal(config)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(mapping, raw, 0600); e != nil {
		t.Fatal(e)
	}
	oldFlags, oldArgs := flag.CommandLine, os.Args
	defer func() { flag.CommandLine = oldFlags; os.Args = oldArgs }()
	invoke := func() error {
		flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
		os.Args = []string{"investmentaudit", "-file", csv, "-combined", "-mapping", mapping, "-financial-preview", output, "-resolution-report", resolution}
		return run()
	}
	if e = invoke(); e != nil {
		t.Fatal(e)
	}
	resolutionStat, e := os.Stat(resolution)
	if e != nil || resolutionStat.Mode().Perm() != 0600 {
		t.Fatal("private resolution", resolutionStat, e)
	}
	resolutionRaw, e := os.ReadFile(resolution)
	if e != nil {
		t.Fatal(e)
	}
	var inventory imp.SourceResolution
	if e = json.Unmarshal(resolutionRaw, &inventory); e != nil || inventory.Prepared != 1 || inventory.Unresolved != 0 {
		t.Fatal(inventory, e)
	}
	if e = writePrivateJSON(resolution, inventory); e == nil {
		t.Fatal("existing resolution overwritten")
	}
	stat, e := os.Stat(output)
	if e != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("private output", stat, e)
	}
	raw, e = os.ReadFile(output)
	if e != nil {
		t.Fatal(e)
	}
	var report imp.FinancialMappingReport
	if e = json.Unmarshal(raw, &report); e != nil || report.Ready != 1 || report.Rows[0].Operation.Amount != "100" {
		t.Fatal(report, e)
	}
	if e = invoke(); e == nil {
		t.Fatal("existing review overwritten")
	}
	after, e := os.ReadFile(output)
	if e != nil || string(raw) != string(after) {
		t.Fatal("existing file changed", e)
	}
}
