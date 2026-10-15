package nativehistory

import (
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
)

func TestBuiltinBenchmarkIdentityAndNamespaces(t *testing.T) {
	for _, b := range Benchmarks() {
		if Identity(b.Provider, b.Instrument) != nil || md.ValidateInstrument(b.Instrument) != nil {
			t.Fatal(b)
		}
		other := b.Instrument
		other.Kind = "share"
		if Identity(b.Provider, other) == nil {
			t.Fatal("index/ETF substituted by stock", b.Key)
		}
		other = b.Instrument
		other.ClassCode = "OTHER"
		if Identity(b.Provider, other) == nil {
			t.Fatal("unreviewed listing accepted")
		}
	}
}
