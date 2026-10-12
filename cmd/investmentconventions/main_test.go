package main

import (
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"strings"
	"testing"
)

func TestProfileParserRejectsAmbiguousInput(t *testing.T) {
	for _, input := range []string{`{"UnknownField":true}`, `{"Periods":{"S":[{"Policy":{"MaximumQuoteAge":5}}]}}`, `{} {}`, `null`, strings.Repeat(" ", 4*1024*1024+1)} {
		if _, e := readProfile(strings.NewReader(input)); e == nil {
			t.Fatal("malformed profile accepted")
		}
	}
	if p, e := readProfile(strings.NewReader(`{"Key":"fixture","Title":"Fixture","Evidence":"Synthetic","Enabled":false,"Periods":{}}`)); e != nil || p.Key != "fixture" {
		t.Fatal(p, e)
	}
}

func TestBenchmarkParserRejectsUnknownConvention(t *testing.T) {
	if _, e := readReviewedProfile[nav.BenchmarkProfile](strings.NewReader(`{"Kind":"price_index","PriceProfile":"misspelled"}`)); e == nil {
		t.Fatal("ignored unknown benchmark field")
	}
	p, e := readReviewedProfile[nav.BenchmarkProfile](strings.NewReader(`{"Key":"imoex","Title":"IMOEX price index","Kind":"price_index","Currency":"RUB","Evidence":"Verified fixture","PriceProfileKey":"prices","InstrumentKey":"imoex","Enabled":true}`))
	if e != nil || !nav.ValidateBenchmarkProfile(p) {
		t.Fatal(p, e)
	}
}
