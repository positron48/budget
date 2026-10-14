package tinvest

import "testing"

func TestQuoteIdentitySeparatesMoscowAndForeignCoincidentTickers(t *testing.T) {
	candidates := []Instrument{{UID: "moscow", Ticker: "TEST", ClassCode: "TQBR", InstrumentType: "share"}, {UID: "foreign", Ticker: "TEST", ClassCode: "SPBXM", InstrumentType: "share"}, {UID: "synthetic", Ticker: "TEST", ClassCode: "SPBXML01", InstrumentType: "share"}}
	for exchange, want := range map[string]string{"MCX": "moscow", "NYSE": "foreign"} {
		item, e := SelectQuoteInstrument(QuoteIdentifier{Key: "key", Ticker: "TEST", Exchange: exchange}, candidates)
		if e != nil || item.UID != want {
			t.Fatal(item, e)
		}
	}
	candidates = append(candidates, Instrument{UID: "second", Ticker: "TEST", ClassCode: "SPBXM", InstrumentType: "share"})
	if _, e := SelectQuoteInstrument(QuoteIdentifier{Key: "key", Ticker: "TEST", Exchange: "NYSE"}, candidates); e == nil {
		t.Fatal("ambiguous listings selected")
	}
}
func TestQuoteIdentityRespectsKnownISINAndUID(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	candidate := Instrument{UID: id, Ticker: "TEST", ISIN: "US0000000001", ClassCode: "TQCB", InstrumentType: "bond"}
	q := QuoteIdentifier{Key: "key", Ticker: "TEST", Exchange: "MCX", UID: id, ISIN: candidate.ISIN}
	if _, e := SelectQuoteInstrument(q, []Instrument{candidate}); e != nil {
		t.Fatal(e)
	}
	q.ISIN = "US0000000002"
	if _, e := SelectQuoteInstrument(q, []Instrument{candidate}); e == nil {
		t.Fatal("conflicting ISIN accepted")
	}
}

func TestCrossVenueQuoteNeedsConfirmedCommonShareISIN(t *testing.T) {
	candidate := Instrument{UID: "foreign", Ticker: "TEST", ISIN: "US0000000001", ClassCode: "SPBXM", InstrumentType: "share"}
	q := QuoteIdentifier{Key: "TEST@LSE", Ticker: "TEST", Exchange: "LSE"}
	if _, e := SelectQuoteInstrument(q, []Instrument{candidate}); e == nil {
		t.Fatal("cross-venue ticker alone accepted")
	}
	q.ISIN = candidate.ISIN
	if _, e := SelectQuoteInstrument(q, []Instrument{candidate}); e != nil {
		t.Fatal(e)
	}
}
