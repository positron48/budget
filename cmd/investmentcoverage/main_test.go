package main

import (
	"github.com/positron48/budget/internal/adapter/tinvest"
	"github.com/positron48/budget/internal/usecase/investmentimport"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceIdentityAndDateRange(t *testing.T) {
	stage := investmentimport.StagedSource{Rows: []investmentimport.SourceRow{
		{Raw: map[string]string{"Symbol": "ABC", "Exchange": "X", "Date": "2022-01-03 13:00:00"}},
		{Raw: map[string]string{"Symbol": "ABC", "Exchange": "Y", "Date": "2021-01-03 13:00:00"}},
		{Raw: map[string]string{"Symbol": "ABC", "Exchange": "X", "Date": "2020-01-03 13:00:00"}},
	}}
	rows, err := sourceInstruments(stage)
	if err != nil || len(rows) != 2 || rows[0].FirstDay != "2020-01-03" || rows[0].LastDay != "2022-01-03" || rows[1].Exchange != "Y" {
		t.Fatalf("%+v %v", rows, err)
	}
	if exactCandidate("ABC", tinvest.Instrument{Ticker: "ABCD"}) || !exactCandidate("abc", tinvest.Instrument{Ticker: "ABC"}) || !exactCandidate("US123", tinvest.Instrument{ISIN: "US123"}) {
		t.Fatal("candidate selection")
	}
}
func TestPrivateCheckpoint(t *testing.T) {
	p := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(p, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := save(p, report{Version: 1}); err != nil {
		t.Fatal(err)
	}
	s, _ := os.Stat(p)
	if s.Mode().Perm() != 0600 {
		t.Fatal("checkpoint is not private")
	}
}

func TestSummaryDoesNotConflateListingsOrFailures(t *testing.T) {
	r := report{Entries: []entry{
		{Complete: true, Candidates: []candidate{{Prices: []tinvest.LastPrice{{}}}, {Candles: []tinvest.Candle{{}}}}},
		{Complete: false, Candidates: []candidate{{Prices: []tinvest.LastPrice{{}}, Candles: []tinvest.Candle{{}}}}},
		{Complete: true},
	}}
	s := summarize(r)
	if s.Instruments != 3 || s.Pending != 1 || s.NoCandidate != 1 || s.MultipleCandidates != 1 || s.WithPrice != 1 || s.WithSampleHistory != 1 || s.WithBothOnSameCandidate != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestCashRowsAreNotMissingSecurities(t *testing.T) {
	cash := investmentimport.SourceRow{Raw: map[string]string{"Symbol": "RUB", "Currency": "RUB", "Exchange": "", "Event": "CASH_IN", "Date": "2020-01-01 00:00:00"}}
	rows, err := sourceInstruments(investmentimport.StagedSource{Rows: []investmentimport.SourceRow{cash}})
	if err != nil || len(rows) != 1 || !rows[0].CashOnly {
		t.Fatal(rows, err)
	}
	security := investmentimport.SourceRow{Raw: map[string]string{"Symbol": "RUB", "Currency": "RUB", "Exchange": "", "Event": "BUY", "Date": "2020-01-01 00:00:00"}}
	rows, err = sourceInstruments(investmentimport.StagedSource{Rows: []investmentimport.SourceRow{cash, security}})
	if err != nil || rows[0].CashOnly {
		t.Fatal("ambiguous row group treated as cash", rows, err)
	}
	s := summarize(report{Entries: []entry{{Source: sourceInstrument{CashOnly: true}, Complete: true}}})
	if s.CashCurrencies != 1 || s.Securities != 0 || s.NoCandidate != 0 {
		t.Fatal(s)
	}
}
