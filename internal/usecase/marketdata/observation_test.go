package marketdata

import (
	"testing"
	"time"
)

func validObservation() Observation {
	return Observation{InstrumentID: "11111111-1111-4111-8111-111111111111", Kind: "daily_candle", At: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), SourceKind: "exchange", PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Price: "9007199254740993.000000001", Open: "9007199254740993", Low: "9007199254740992", High: "9007199254740994", Volume: "0001", Complete: true}
}
func TestNormalizeProviderEvidence(t *testing.T) {
	o, err := Normalize(validObservation())
	if err != nil || o.Price != "9007199254740993.000000001" || o.Volume != "1" || o.Currency != "" {
		t.Fatal(o, err)
	}
	for name, change := range map[string]func(*Observation){
		"OHLC": func(o *Observation) { o.High = "2" }, "precision": func(o *Observation) { o.Price = "1.1234567891" },
		"submicrosecond": func(o *Observation) { o.At = o.At.Add(time.Nanosecond) }, "unknown kind": func(o *Observation) { o.Kind = "fx" },
		"currency": func(o *Observation) { o.Currency = "rub" }, "unknown units omitted": func(o *Observation) { o.PriceUnit = "" },
		"volume overflow": func(o *Observation) { o.Volume = "9223372036854775808" }, "quote fields": func(o *Observation) { o.Kind = "last_price" },
	} {
		t.Run(name, func(t *testing.T) {
			v := validObservation()
			change(&v)
			if _, err := Normalize(v); err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}
