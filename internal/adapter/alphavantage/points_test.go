package alphavantage

import (
	"strings"
	"testing"
	"time"

	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func retrospectiveFixture() (md.FXPolicy, Audit) {
	from := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	p := md.FXPolicy{Key: "alpha-retrospective", Mode: "market", Source: "alphavantage", AccessScope: "personal", SourceReference: "Reviewed personal FX_DAILY", TimeConvention: md.AlphaTimeConvention, ValueConvention: md.AlphaValueConvention, ValidityConvention: md.AlphaValidityConvention, Alpha: &md.AlphaFXPolicy{From: from, Convention: md.AlphaRetrospectiveConvention}}
	a := Audit{Source: "alphavantage", Pair: "USD/RUB", From: "2024-01-05", To: "2024-01-07", Timezone: "UTC", LastRefreshed: "2024-01-05", BodySHA256: strings.Repeat("a", 64), RetrievedAt: from.Add(96 * time.Hour), Bars: []Bar{{Date: "2024-01-05", Open: "90", High: "92", Low: "89", Close: "91.12345678901"}}}
	return p, a
}
func TestRetrospectivePointsBoundariesAndEvidence(t *testing.T) {
	p, a := retrospectiveFixture()
	points, err := RetrospectivePoints(p, a)
	if err != nil || len(points) != 1 {
		t.Fatal(points, err)
	}
	f := points[0]
	midnight := p.Alpha.From.Add(24 * time.Hour)
	if f.Rate != a.Bars[0].Close || !f.At.Equal(midnight) || !f.AvailableAt.Equal(midnight) || !f.ValidUntil.Equal(midnight.Add(96*time.Hour)) || f.EvidenceSHA256 == a.BodySHA256 || !strings.Contains(f.RecordReference, a.BodySHA256) {
		t.Fatal(f)
	}
	fixing := md.SavedFXPoint{PolicyKey: p.Key, PolicyVersion: 1, FXPoint: f, ID: "checkpoint", Revision: 1}.Fixing()
	for _, tc := range []struct {
		at   time.Time
		want bool
	}{{midnight.Add(-time.Nanosecond), false}, {midnight, true}, {midnight.Add(48 * time.Hour), true}, {f.ValidUntil.Add(-time.Nanosecond), true}, {f.ValidUntil, false}} {
		r, e := calc.NewFXResolver([]calc.FXFixing{fixing}, md.FXPolicyRef(p.Key, 1), tc.at)
		if e != nil {
			t.Fatal(e)
		}
		_, ok := r.Resolve("USD", "RUB")
		if ok != tc.want {
			t.Fatalf("at=%s got=%v", tc.at, ok)
		}
	}
	changed := a
	changed.Bars = append([]Bar(nil), a.Bars...)
	changed.Bars[0].Close = "91"
	revised, e := RetrospectivePoints(p, changed)
	if e != nil || revised[0].EvidenceSHA256 == f.EvidenceSHA256 {
		t.Fatal("normalized checkpoint mutation must change evidence identity", e)
	}
}
func TestRetrospectivePointsRejectUnreviewedInputs(t *testing.T) {
	for _, change := range []func(*md.FXPolicy, *Audit){
		func(p *md.FXPolicy, _ *Audit) { p.Alpha = nil },
		func(p *md.FXPolicy, _ *Audit) { p.TimeConvention = "Published at midnight" },
		func(p *md.FXPolicy, _ *Audit) { p.Mode = "cbr" },
		func(p *md.FXPolicy, _ *Audit) { p.AccessScope = "public" },
		func(_ *md.FXPolicy, a *Audit) { a.Pair = "EUR/USD" },
		func(_ *md.FXPolicy, a *Audit) { a.RetrievedAt = time.Date(2024, 1, 5, 12, 0, 0, 0, time.UTC) },
		func(p *md.FXPolicy, _ *Audit) { p.Alpha.From = p.Alpha.From.Add(24 * time.Hour) },
	} {
		p, a := retrospectiveFixture()
		change(&p, &a)
		if _, err := RetrospectivePoints(p, a); err == nil {
			t.Fatal("accepted invalid retrospective evidence")
		}
	}
}
