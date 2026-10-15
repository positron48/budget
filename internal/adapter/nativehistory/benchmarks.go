package nativehistory

import (
	"github.com/positron48/budget/internal/adapter/moex"
	td "github.com/positron48/budget/internal/adapter/twelvedata"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"time"
)

// Builtins are reviewed public identities, independent of personal mappings.
// They grant no entitlement: only an explicitly enabled matching source is read.
type Benchmark struct {
	Key, Title, Kind, Provider, AccessScope, URL, Unit string
	Instrument                                         md.Instrument
	From                                               time.Time
}

func Benchmarks() []Benchmark {
	index := moex.Listing{SecurityID: "IMOEX", Board: "SNDX"}
	spy := td.EquityListing{Symbol: "SPY", Exchange: "NYSE", MIC: "ARCX"}
	return []Benchmark{
		{Key: "imoex", Title: "IMOEX", Kind: "price_index", Provider: "moex", AccessScope: "personal", URL: "https://www.moex.com/ru/index/IMOEX", Unit: "index_points", From: time.Date(1997, 9, 22, 0, 0, 0, 0, time.UTC), Instrument: md.Instrument{ID: index.IndexID(), Detailed: true, Ticker: "IMOEX", ClassCode: "SNDX", Name: "MOEX Russia Index", Kind: "index", Currency: "RUB"}},
		{Key: "spy", Title: "SPY · S&P 500 ETF proxy", Kind: "etf_price_proxy", Provider: "twelvedata", AccessScope: "personal", URL: "https://www.ssga.com/us/en/institutional/etfs/state-street-spdr-sp-500-etf-trust-spy", Unit: "currency_per_unit", From: time.Date(1993, 1, 22, 0, 0, 0, 0, time.UTC), Instrument: md.Instrument{ID: spy.ID(), Detailed: true, Ticker: "SPY", ClassCode: "NYSE:ARCX", ISIN: "US78462F1030", Name: "SPDR S&P 500 ETF Trust", Kind: "etf", Currency: "USD"}},
	}
}
func (b Benchmark) Period() md.MappingPeriod {
	return md.MappingPeriod{Provider: b.Provider, AccessScope: b.AccessScope, InstrumentID: b.Instrument.ID, Kind: b.Instrument.Kind, Currency: b.Instrument.Currency, Unit: b.Unit, From: b.From, Evidence: b.URL}
}
