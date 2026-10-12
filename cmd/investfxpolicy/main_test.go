package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewedPolicyPlanRejectsUnknownFieldsAndBindsActor(t *testing.T) {
	p := filepath.Join(t.TempDir(), "policy.json")
	tenant := "11111111-1111-4111-8111-111111111111"
	actor := "22222222-2222-4222-8222-222222222222"
	raw := `{"ExpectedVersion":0,"Policy":{"Key":"market","Mode":"market","Source":"tinvest","AccessScope":"personal","SourceReference":"Issuer fixture","TimeConvention":"Reviewed offsets","ValueConvention":"Per base unit","ValidityConvention":"Reviewed expiry","Series":[{"InstrumentID":"33333333-3333-4333-8333-333333333333","FromCurrency":"USD","ToCurrency":"RUB","From":"2020-01-01T00:00:00Z","To":"2024-06-13T00:00:00Z","RateKind":"exchange_spot","SourceKind":"exchange","PriceUnit":"provider_unspecified","Adjustment":"provider_unspecified","Multiplier":"1","Divisor":"1000","EffectiveOffsetSeconds":86400,"AvailableOffsetSeconds":86400,"ValidSeconds":86400,"RequirePositiveVolume":true,"SourceReference":"Historical terms","Evidence":"Synthetic reviewed period"}]}}`
	if e := os.WriteFile(p, []byte(raw), 0600); e != nil {
		t.Fatal(e)
	}
	_, first, e := loadPlan(p, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	_, other, e := loadPlan(p, tenant, tenant)
	if e != nil || other == first {
		t.Fatal("plan not bound to actor", e)
	}
	if e = os.WriteFile(p, []byte(strings.Replace(raw, `"ExpectedVersion":0`, `"ExpectedVersion":1`, 1)), 0600); e != nil {
		t.Fatal(e)
	}
	_, changed, e := loadPlan(p, tenant, actor)
	if e != nil || changed == first {
		t.Fatal("plan not bound to version", e)
	}
	for _, invalid := range []string{raw + `{}`, strings.Replace(raw, `"ExpectedVersion":0`, `"TypoVersion":0`, 1), strings.Replace(raw, `"Mode":"market"`, `"Mode":"cbr"`, 1)} {
		if e = os.WriteFile(p, []byte(invalid), 0600); e != nil {
			t.Fatal(e)
		}
		if _, _, e = loadPlan(p, tenant, actor); e == nil {
			t.Fatal("invalid source plan accepted")
		}
	}
}

func TestOfficialPolicyPreview(t *testing.T) {
	raw, e := os.ReadFile("testdata/cbr-policy.json")
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "official.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	p, hash, e := loadPlan(path, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	if e != nil || hash == "" || p.Document.Policy.Official == nil || p.Document.Policy.Mode != "cbr" {
		t.Fatal(e, p)
	}
	for _, invalid := range []string{strings.Replace(string(raw), `"Mode": "cbr"`, `"Mode": "market"`, 1), strings.Replace(string(raw), `"AccessScope": "public"`, `"AccessScope": "personal"`, 1), strings.Replace(string(raw), `"Convention": "effective_moscow_date_v1"`, `"Convention": "unknown"`, 1)} {
		if e = os.WriteFile(path, []byte(invalid), 0600); e != nil {
			t.Fatal(e)
		}
		if _, _, e = loadPlan(path, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"); e == nil {
			t.Fatal("invalid official configuration accepted")
		}
	}
}
