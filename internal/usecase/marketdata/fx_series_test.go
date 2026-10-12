package marketdata

import (
	"encoding/json"
	"github.com/google/uuid"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"strings"
	"testing"
	"time"
)

func seriesFixture() (FXPolicy, SavedObservation) {
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	id := uuid.NewString()
	s := FXSeries{InstrumentID: id, FromCurrency: "USD", ToCurrency: "RUB", From: day, To: day.AddDate(0, 0, 10), RateKind: "exchange_spot", SourceKind: "exchange", PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Multiplier: "1", Divisor: "1000", EffectiveOffsetSeconds: 86400, AvailableOffsetSeconds: 86400, ValidSeconds: 3 * 86400, RequirePositiveVolume: true, SourceReference: "Historical nominal", Evidence: "Synthetic source review"}
	p := FXPolicy{Key: "market", Mode: "market", Source: "tinvest", AccessScope: "personal", SourceReference: "Issuer", TimeConvention: "Reviewed offsets", ValueConvention: "Per base unit", ValidityConvention: "Reviewed carry", Series: []FXSeries{s}}
	o := SavedObservation{ID: uuid.NewString(), Revision: 1, ReceivedAt: day.Add(48 * time.Hour), Observation: Observation{InstrumentID: id, Kind: "daily_candle", At: day, SourceKind: "exchange", Currency: "RUB", PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Price: "90000.123456789", Open: "90000.123456789", High: "90000.123456789", Low: "90000.123456789", Volume: "1", Complete: true}}
	return p, o
}
func TestReviewedFXSeriesExactNominalAndRetraction(t *testing.T) {
	p, o := seriesFixture()
	if e := ValidateFXPolicy(p); e != nil {
		t.Fatal(e)
	}
	point, ok, e := FXSeriesPoint(p.Series[0], o)
	if e != nil || !ok || point.Rate != "90.000123456789" || point.Withdrawn || !point.At.Equal(o.At.Add(24*time.Hour)) || !strings.Contains(point.RecordReference, o.ID+"@1") {
		t.Fatal(e, point)
	}
	for _, change := range []func(*SavedObservation){func(o *SavedObservation) { o.Complete = false }, func(o *SavedObservation) { o.Currency = "USD" }, func(o *SavedObservation) { o.Volume = "0" }, func(o *SavedObservation) { o.ReceivedAt = o.At }} {
		bad := o
		change(&bad)
		v, matched, e := FXSeriesPoint(p.Series[0], bad)
		if e != nil || !matched || !v.Withdrawn || v.Rate != "" {
			t.Fatal(e, v)
		}
	}
	old := SavedFXPoint{FXPoint: point, PolicyKey: p.Key, PolicyVersion: 1, ID: uuid.NewString(), Revision: 1}
	newer := old
	newer.At = old.At.Add(time.Hour)
	newer.AvailableAt = newer.At
	newer.Withdrawn = true
	newer.Rate = ""
	newer.ID = uuid.NewString()
	r, e := calc.NewFXResolver([]calc.FXFixing{old.Fixing(), newer.Fixing()}, FXPolicyRef(p.Key, 1), newer.At.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if _, ok = r.Resolve("USD", "RUB"); ok {
		t.Fatal("withdrawal resurrected older fixing")
	}
	p.Series[0].Divisor = "3"
	o.Price = "1"
	o.Open = "1"
	o.High = "1"
	o.Low = "1"
	point, _, e = FXSeriesPoint(p.Series[0], o)
	if e != nil || !point.Withdrawn {
		t.Fatal("precision was rounded", e, point)
	}
	p.Series[0].Divisor = "0.000000000000000001"
	p.Series[0].Multiplier = "999999999999999999999999"
	point, _, e = FXSeriesPoint(p.Series[0], o)
	if e != nil || !point.Withdrawn || point.Rate != "" {
		t.Fatal("out-of-range rate must retract without losing raw data", e, point)
	}
	o.At = p.Series[0].To
	if _, matched, e := FXSeriesPoint(p.Series[0], o); e != nil || matched {
		t.Fatal("period end accepted", e)
	}
}
func TestReviewedFXSeriesRejectsUnreviewedAndAmbiguousBindings(t *testing.T) {
	p, _ := seriesFixture()
	next := p.Series[0]
	next.InstrumentID = uuid.NewString()
	next.From = p.Series[0].To
	next.To = next.From.AddDate(0, 0, 10)
	next.EffectiveOffsetSeconds = 0
	p.Series = append(p.Series, next)
	if ValidateFXPolicy(p) == nil {
		t.Fatal("overlapping effective periods accepted")
	}
	for _, change := range []func(*FXPolicy){func(p *FXPolicy) { p.Mode = "cbr" }, func(p *FXPolicy) { p.Series[0].RateKind = "official" }, func(p *FXPolicy) { p.Series[0].Evidence = "" }, func(p *FXPolicy) { p.Series[0].Divisor = "0" }, func(p *FXPolicy) { p.Series[0].AvailableOffsetSeconds = 0 }, func(p *FXPolicy) { p.Series = append(p.Series, p.Series[0]) }, func(p *FXPolicy) { s := p.Series[0]; s.FromCurrency = "EUR"; p.Series = append(p.Series, s) }} {
		p, _ := seriesFixture()
		change(&p)
		if ValidateFXPolicy(p) == nil {
			t.Fatal("unreviewed/ambiguous policy accepted", p)
		}
	}
	// Optional fields preserve legacy policy and point JSON checksums exactly.
	p, _ = seriesFixture()
	p.Series = nil
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "Series") {
		t.Fatal("legacy policy JSON changed")
	}
	point := FXPoint{}
	raw, _ = json.Marshal(point)
	if strings.Contains(string(raw), "Withdrawn") {
		t.Fatal("legacy fixing JSON changed")
	}
}
