package tinvest

import (
	"encoding/json"
	"testing"
	"time"
)

func TestObservationConversionPreservesUnknownUnits(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	p, err := PriceObservation(LastPrice{InstrumentUID: id, Time: at, Type: "LAST_PRICE_EXCHANGE", Price: Quotation{Units: json.Number("95"), Nano: 1}})
	if err != nil || p.Price != "95.000000001" || p.PriceUnit != "provider_unspecified" || p.Currency != "" || p.Adjustment != "unadjusted" {
		t.Fatal(p, err)
	}
	candle := Candle{Time: at, Source: "CANDLE_SOURCE_EXCHANGE", Open: Quotation{Units: "95"}, High: Quotation{Units: "96"}, Low: Quotation{Units: "94"}, Close: Quotation{Units: "95"}, Complete: false}
	observations, err := CandleObservations(id, DailyHistory{PriceCurrency: "rub", Candles: []Candle{candle}})
	if err != nil || len(observations) != 1 || observations[0].Currency != "RUB" || observations[0].Complete || observations[0].Adjustment != "provider_unspecified" || observations[0].At != at {
		t.Fatal(observations, err)
	}
	candle.Source = "CANDLE_SOURCE_UNSPECIFIED"
	if _, err = CandleObservations(id, DailyHistory{Candles: []Candle{candle}}); err == nil {
		t.Fatal("unknown provenance accepted")
	}
}

func TestPointCurrencyRemainsUnresolved(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	candle := Candle{Time: time.Date(2025, 4, 15, 0, 0, 0, 0, time.UTC), Source: "CANDLE_SOURCE_EXCHANGE", Open: Quotation{Units: "95"}, High: Quotation{Units: "95"}, Low: Quotation{Units: "95"}, Close: Quotation{Units: "95"}, Complete: true}
	got, err := CandleObservations(id, DailyHistory{PriceCurrency: "pt.", Candles: []Candle{candle}})
	if err != nil || len(got) != 1 || got[0].Currency != "" || got[0].PriceUnit != "provider_unspecified" || got[0].Price != "95.000000000" {
		t.Fatal("point price was rejected or assigned currency/nominal", err)
	}
	if _, err = CandleObservations(id, DailyHistory{PriceCurrency: "unknown", Candles: []Candle{candle}}); err == nil {
		t.Fatal("unknown currency marker accepted")
	}
}
