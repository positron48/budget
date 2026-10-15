package investmentcalc

import (
	"math"
	"math/big"
	"testing"
	"time"
)

func TestXIRRLongReinvestingStrategyCertifiedWithoutRootSampling(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, rate := range []float64{.1, -.1} {
		flows := []DatedCashFlow{{Date: start.Format(time.DateOnly), Amount: big.NewRat(-10000, 1)}}
		terminal := 10000 * (1 + rate)
		for i := 1; i <= 300; i++ {
			value := int64(-1)
			if i%2 == 1 {
				value = 1
			}
			flows = append(flows, DatedCashFlow{Date: start.AddDate(0, 0, i).Format(time.DateOnly), Amount: big.NewRat(value, 1)})
			terminal -= float64(value) * math.Pow(1+rate, float64(365-i)/365)
		}
		flows = append(flows, DatedCashFlow{Date: start.AddDate(0, 0, 365).Format(time.DateOnly), Amount: new(big.Rat).SetFloat64(terminal)})
		got := XIRR(flows)
		if got.Rate == nil || math.Abs(*got.Rate-rate) > 1e-10 {
			t.Fatal(rate, got)
		}
	}
}

func TestXIRRCumulativeCertificateDoesNotHideMultipleLossRoots(t *testing.T) {
	keys := []string{"a", "b", "c"}
	values := map[string]*big.Rat{"a": big.NewRat(-100, 1), "b": big.NewRat(99, 1), "c": big.NewRat(-1, 1)}
	if _, ok := irrCumulativeCertificate(keys, values); ok {
		t.Fatal("negative prefixes alone do not prove a unique loss root")
	}
	got := XIRR([]DatedCashFlow{{"2024-01-01", values["a"]}, {"2024-12-31", values["b"]}, {"2025-12-31", values["c"]}})
	if got.Rate != nil || got.Reason != "multiple_roots" {
		t.Fatal(got)
	}
}
