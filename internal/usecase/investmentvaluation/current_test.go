package investmentvaluation

import (
	"testing"
	"time"
)

func TestCurrentPricePolicyRequiresExplicitSLA(t *testing.T) {
	daily := SeriesPolicy{Kind: "daily_candle", SourceKind: "exchange", Adjustment: "unadjusted"}
	if _, ok := daily.CurrentPolicy(); ok {
		t.Fatal("implicit daily fallback")
	}
	daily.CurrentMaxQuoteAge = time.Hour
	current, ok := daily.CurrentPolicy()
	if !ok || current.Kind != "last_price" || current.Calendar != nil || current.MaxQuoteAge != time.Hour || current.SourceKind != daily.SourceKind || current.Adjustment != daily.Adjustment {
		t.Fatal(current, ok)
	}
	if daily.Kind != "daily_candle" {
		t.Fatal("changed historical convention")
	}
	daily.CurrentMaxQuoteAge = 31 * 24 * time.Hour
	if _, ok := daily.CurrentPolicy(); ok {
		t.Fatal("unbounded SLA")
	}
	if _, ok := (SeriesPolicy{Kind: "last_price"}).CurrentPolicy(); ok {
		t.Fatal("missing SLA")
	}
	if _, ok := (SeriesPolicy{Kind: "last_price", MaxQuoteAge: time.Hour}).CurrentPolicy(); !ok {
		t.Fatal("reviewed last-price rejected")
	}
}
