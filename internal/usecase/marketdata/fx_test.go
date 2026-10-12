package marketdata

import (
	"strings"
	"testing"
	"time"
)

func TestFXEvidenceValidation(t *testing.T) {
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	good := FXPoint{From: "USD", To: "RUB", Rate: "00090.000000000000000001", At: at, AvailableAt: at, ValidUntil: at.Add(time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: "raw-response/record-1"}
	normalized, e := NormalizeFXPoint(good)
	if e != nil || normalized.Rate != "90.000000000000000001" {
		t.Fatal(normalized, e)
	}
	for _, change := range []func(*FXPoint){func(p *FXPoint) { p.Rate = "0" }, func(p *FXPoint) { p.Rate = "1/3" }, func(p *FXPoint) { p.EvidenceSHA256 = "" }, func(p *FXPoint) { p.RecordReference = "" }, func(p *FXPoint) { p.AvailableAt = at.Add(-time.Second) }, func(p *FXPoint) { p.ValidUntil = at }, func(p *FXPoint) { p.At = at.Add(time.Nanosecond) }, func(p *FXPoint) { p.To = "USD" }} {
		bad := good
		change(&bad)
		if _, e := NormalizeFXPoint(bad); e == nil {
			t.Fatal("invalid FX point accepted", bad)
		}
	}
}
