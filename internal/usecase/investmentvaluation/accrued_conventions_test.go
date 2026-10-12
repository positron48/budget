package investmentvaluation

import (
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"strings"
	"testing"
	"time"
)

func TestReviewedAccruedBoundariesAndScope(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	price := PriceConventionPeriod{Scope: scope, InstrumentID: tenant, From: at, Policy: SeriesPolicy{Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", MaxQuoteAge: 24 * time.Hour}}
	v := ReviewedAccruedPeriod{Scope: scope, InstrumentID: tenant, SnapshotID: tenant, SnapshotHash: strings.Repeat("a", 64), PointAt: at, From: at, To: at.Add(24 * time.Hour), Currency: "RUB", AmountPerUnit: "0", Evidence: "Verified currency and Moscow date interval"}
	p := ConventionProfile{Key: "bond", Title: "Reviewed bond", Evidence: "Verified terms", Enabled: true, Periods: map[string][]PriceConventionPeriod{"B": {price}}, Accrued: map[string][]ReviewedAccruedPeriod{"B": {v}}}
	if err := ValidateConventionProfile(tenant, p); err != nil {
		t.Fatal(err)
	}
	if f := p.AccruedAt("B", at, scope, tenant, 2); f == nil || f.AmountPerUnit != "0" || !strings.Contains(f.SourceID, ":2/") {
		t.Fatal(f)
	}
	for _, date := range []time.Time{at.Add(-time.Microsecond), v.To, v.To.Add(time.Hour)} {
		if f := p.AccruedAt("B", date, scope, tenant, 2); f != nil {
			t.Fatal("carried point across gap", f)
		}
	}
	other := scope
	other.AccessScope = "other"
	if p.AccruedAt("B", at, other, tenant, 2) != nil {
		t.Fatal("scope mismatch")
	}
	for _, mutate := range []func(*ReviewedAccruedPeriod){func(v *ReviewedAccruedPeriod) { v.To = v.From.Add(49 * time.Hour) }, func(v *ReviewedAccruedPeriod) { v.Currency = "rub" }, func(v *ReviewedAccruedPeriod) { v.AmountPerUnit = "-1" }, func(v *ReviewedAccruedPeriod) { v.AmountPerUnit = "1/3" }, func(v *ReviewedAccruedPeriod) { v.PointAt = v.To }, func(v *ReviewedAccruedPeriod) { v.SnapshotHash = "unverified" }, func(v *ReviewedAccruedPeriod) { v.AccessScope = "other" }, func(v *ReviewedAccruedPeriod) { v.Evidence = "" }} {
		bad := v
		mutate(&bad)
		p.Accrued["B"] = []ReviewedAccruedPeriod{bad}
		if ValidateConventionProfile(tenant, p) == nil {
			t.Fatal("invalid review accepted", bad)
		}
	}
	p.Accrued["B"] = []ReviewedAccruedPeriod{v, v}
	if ValidateConventionProfile(tenant, p) == nil {
		t.Fatal("overlap accepted")
	}
	if !SameAccruedAmount("9007199254740993.000000001", "9007199254740993.000000001") || SameAccruedAmount("9007199254740993.000000001", "9007199254740993.000000002") {
		t.Fatal("precision lost")
	}
}
