package nativehistory

import (
	"context"
	"errors"
	"testing"
	"time"

	td "github.com/positron48/budget/internal/adapter/twelvedata"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type testEquities struct {
	from, to     string
	listing      td.EquityListing
	observations []md.Observation
	calls        int
}

func (f *testEquities) LoadEquityHistory(_ context.Context, l td.EquityListing, from, to string) (td.EquityAudit, []md.Observation, error) {
	f.calls++
	f.from, f.to, f.listing = from, to, l
	return td.EquityAudit{}, f.observations, nil
}

func TestNativeHistoryUsesCanonicalIdentityAndOnlyClosedExchangeDates(t *testing.T) {
	l := td.EquityListing{Symbol: "SYNTH", Exchange: "NYSE", MIC: "XNYS"}
	i := md.Instrument{ID: l.ID(), Detailed: true, Ticker: l.Symbol, ClassCode: "NYSE:XNYS", Name: "Synthetic", Kind: "share", Currency: "USD"}
	first := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	f := &testEquities{observations: []md.Observation{{InstrumentID: i.ID, Kind: "daily_candle", At: first, SourceKind: "exchange", Currency: "USD", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Price: "10", Open: "10", High: "10", Low: "10", Volume: "1", Complete: true}}}
	p := &Provider{Scope: md.Scope{TenantID: "11111111-1111-4111-8111-111111111111", Provider: "twelvedata", AccessScope: "personal"}, Lookup: func(context.Context, string) (md.Instrument, error) { return i, nil }, Equities: f, Now: func() time.Time { return time.Date(2024, 1, 4, 2, 0, 0, 0, time.UTC) }}
	q := md.JobRequest{InstrumentID: i.ID, Kind: "daily_candle", From: first, To: first.AddDate(0, 0, 4)}
	got, e := p.LoadMarketObservations(context.Background(), q)
	if e != nil || len(got) != 1 || f.from != "2024-01-01" || f.to != "2024-01-02" || f.listing != l {
		t.Fatal(got, f, e)
	}
	i.ClassCode = "NASDAQ:XNGS"
	if _, e = p.LoadMarketObservations(context.Background(), q); !errors.Is(e, md.ErrInvalid) || f.calls != 1 {
		t.Fatal("venue mismatch reached provider", e, f.calls)
	}
	i.ClassCode = "NYSE:XNYS"
	q.Kind = "last_price"
	q.From, q.To = time.Time{}, time.Time{}
	if _, e = p.LoadMarketObservations(context.Background(), q); !errors.Is(e, md.ErrInvalid) {
		t.Fatal("live quote request accepted", e)
	}
	if _, e = p.SearchMarketCatalog(context.Background(), md.JobRequest{}); !errors.Is(e, md.ErrInvalid) {
		t.Fatal(e)
	}
}

func TestNativeHistoryRejectsWrongSeriesAndPreservesQuality(t *testing.T) {
	l := td.EquityListing{Symbol: "SYNTH", Exchange: "NASDAQ", MIC: "XNGS"}
	i := md.Instrument{ID: l.ID(), Detailed: true, Ticker: l.Symbol, ClassCode: "NASDAQ:XNGS", Name: "Synthetic", Kind: "share", Currency: "USD"}
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	o := md.Observation{InstrumentID: i.ID, Kind: "daily_candle", At: day, SourceKind: "exchange", Currency: "USD", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Price: "10", Open: "12", High: "10", Low: "10", Volume: "1", Complete: true, QualityIssue: "provider_ohlc_conflict"}
	f := &testEquities{observations: []md.Observation{o}}
	p := &Provider{Scope: md.Scope{TenantID: "11111111-1111-4111-8111-111111111111", Provider: "twelvedata", AccessScope: "personal"}, Lookup: func(context.Context, string) (md.Instrument, error) { return i, nil }, Equities: f, Now: func() time.Time { return day.AddDate(0, 0, 10) }}
	q := md.JobRequest{InstrumentID: i.ID, Kind: "daily_candle", From: day, To: day.AddDate(0, 0, 1)}
	got, e := p.LoadMarketObservations(context.Background(), q)
	if e != nil || got[0].QualityIssue != o.QualityIssue {
		t.Fatal(got, e)
	}
	f.observations[0].Adjustment = "provider_unspecified"
	if _, e = p.LoadMarketObservations(context.Background(), q); !errors.Is(e, md.ErrInvalid) {
		t.Fatal("unspecified adjusted series accepted", e)
	}
}
