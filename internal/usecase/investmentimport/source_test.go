package investmentimport

import (
	"bytes"
	"strings"
	"testing"
)

func TestNormalizeSourceDecimal(t *testing.T) {
	for raw, want := range map[string]string{"8,866666666666666E-05": "0.00008866666666666666", "001.2300": "1.23", "-0,000": "0", "1e3": "1000", "-12E-3": "-0.012", "0.1e1": "1", "123e-2": "1.23"} {
		got, err := NormalizeSourceDecimal(raw)
		if err != nil || got != want {
			t.Fatalf("%s: %s %v, want %s", raw, got, err, want)
		}
	}
	for _, raw := range []string{"NaN", "Inf", "1/2", "1.2,3", "1e999999999999999999999", " 1", "1e101", ""} {
		if _, err := NormalizeSourceDecimal(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestStageLosslessCombined(t *testing.T) {
	row := "BUY,2020-01-01 12:00:00,TEST,10,2,USD,0,NASDAQ,,USD,False,\n"
	source := strings.Join(snowballHeader, ",") + "\n" + row + row
	if _, err := StageSnowball(strings.NewReader(source), false); err == nil {
		t.Fatal("missing explicit scope")
	}
	a, err := StageSnowball(strings.NewReader(source), true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := StageSnowball(strings.NewReader(source), true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.OriginalCSV, []byte(source)) || len(a.Rows) != 2 || a.Rows[0].ID == a.Rows[1].ID || a.Rows[1].ID != b.Rows[1].ID || a.Rows[0].Decimals["Price"] != "10" {
		t.Fatal("source or provenance lost")
	}
}
