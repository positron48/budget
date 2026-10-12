package investmentvaluation

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"io"
	"math/big"
	"testing"
)

func readSeriesCSV(t *testing.T, out SeriesCSV) []map[string]string {
	t.Helper()
	zip, e := gzip.NewReader(bytes.NewReader(out.Data))
	if e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(zip)
	if e != nil {
		t.Fatal(e)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if len(raw) != out.UncompressedSize || hex.EncodeToString(sum[:]) != out.SHA256 {
		t.Fatal("checksum/size")
	}
	table, e := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if e != nil {
		t.Fatal(e)
	}
	rows := []map[string]string{}
	for _, values := range table[1:] {
		row := map[string]string{}
		for i, key := range table[0] {
			row[key] = values[i]
		}
		rows = append(rows, row)
	}
	return rows
}
func TestSeriesCSVExactValuesGapsAndDeterministicEnvelope(t *testing.T) {
	days := []calc.PerformanceDay{{Date: "2024-01-01", NAV: big.NewRat(100, 1), Complete: true}, {Date: "2024-01-02", NAV: big.NewRat(120, 1), ExternalFlow: big.NewRat(10, 1), Complete: true}, {Date: "2024-01-03", ExternalFlow: big.NewRat(0, 1)}}
	period, e := calc.CalculatePeriod(calc.PeriodPerformanceInput{Days: days, FlowCoverage: true})
	if e != nil {
		t.Fatal(e)
	}
	p := Performance{PeriodPerformance: period, Days: days, SeriesKey: "series", Currency: "USD", FXPolicyKey: "market", Generation: 17}
	value := func(complete bool) *Result {
		amount := big.NewRat(30, 1)
		if !complete {
			amount = nil
		}
		known := big.NewRat(90, 1)
		if complete {
			known = big.NewRat(120, 1)
		}
		return &Result{Currency: "USD", FXPolicy: "market@2", Components: map[string]ComponentIdentity{"security": {LedgerID: "a", Kind: "position", Key: "S"}, "cash": {LedgerID: "a", Kind: "cash", Key: "USD"}}, PortfolioValue: calc.PortfolioValue{Valuation: calc.Valuation{KnownValue: known, Complete: complete, ComponentValues: map[string]*big.Rat{"security": amount, "cash": big.NewRat(90, 1)}, Issues: func() []calc.QualityIssue {
			if !complete {
				return []calc.QualityIssue{{ComponentID: "security", Code: "price_missing"}}
			}
			return nil
		}()}}}
	}
	history := []DailyNAV{{Date: "2024-01-02", State: "ready", Generation: 15, Result: value(true)}, {Date: "2024-01-03", State: "incomplete", Generation: 17, Result: value(false)}}
	out, e := EncodeSeriesCSV(p, history, "market")
	if e != nil {
		t.Fatal(e)
	}
	rows := readSeriesCSV(t, out)
	if out.Rows != 2 || out.Generation != 17 || out.Filename != "investments-USD-2024-01-02-2024-01-03.csv" || rows[0]["nav_numerator"] != "120" || rows[0]["daily_profit_numerator"] != "10" || rows[0]["twr_index_numerator"] != "110" || rows[0]["nav_generation"] != "15" || rows[0]["read_generation"] != "17" || rows[0]["baseline_nav_numerator"] != "100" {
		t.Fatal(out, rows)
	}
	if rows[1]["nav_numerator"] != "90" || rows[1]["cash_complete"] != "true" || rows[1]["securities_complete"] != "false" || rows[1]["daily_profit_numerator"] != "" || rows[1]["twr_index_numerator"] != "" || rows[1]["external_flow_numerator"] != "0" || rows[1]["fx_policy_mode"] != "market" || rows[1]["fx_policy_ref"] != "market@2" {
		t.Fatal(rows[1])
	}
	repeat, e := EncodeSeriesCSV(p, history, "market")
	if e != nil || !bytes.Equal(repeat.Data, out.Data) {
		t.Fatal("non-deterministic export", e)
	}
	history[1].State = "stale" // superseded Result must not leak to CSV
	out, e = EncodeSeriesCSV(p, history, "market")
	if e != nil {
		t.Fatal(e)
	}
	rows = readSeriesCSV(t, out)
	if rows[1]["nav_numerator"] != "" || rows[1]["cash_known_numerator"] != "" || rows[1]["fx_policy_ref"] != "" {
		t.Fatal(rows[1])
	}
	history[1].Date = "2024-01-04"
	if _, e = EncodeSeriesCSV(p, history, "market"); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(e)
	}
}
func TestSeriesCSVLimitsRefusePartialWrites(t *testing.T) {
	w := &csvLimitWriter{limit: 3}
	if n, e := w.Write([]byte("ab")); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	if n, e := w.Write([]byte("cd")); n != 0 || !errors.Is(e, d.ErrInvestmentLimit) || w.String() != "ab" {
		t.Fatal(n, e, w.String())
	}
}

func TestSeriesCSVFractionalIndexNeverUsesRoundedDecimal(t *testing.T) {
	days := []calc.PerformanceDay{{Date: "2024-01-01", NAV: big.NewRat(3, 1), Complete: true}, {Date: "2024-01-02", NAV: big.NewRat(4, 1), ExternalFlow: new(big.Rat), Complete: true}}
	period, e := calc.CalculatePeriod(calc.PeriodPerformanceInput{Days: days, FlowCoverage: true})
	if e != nil {
		t.Fatal(e)
	}
	p := Performance{PeriodPerformance: period, Days: days, SeriesKey: "series", Currency: "RUB", FXPolicyKey: "market"}
	result := &Result{Currency: "RUB", Components: map[string]ComponentIdentity{"cash": {LedgerID: "a", Kind: "cash", Key: "RUB"}}, PortfolioValue: calc.PortfolioValue{Valuation: calc.Valuation{KnownValue: big.NewRat(4, 1), Complete: true, ComponentValues: map[string]*big.Rat{"cash": big.NewRat(4, 1)}}}}
	out, e := EncodeSeriesCSV(p, []DailyNAV{{Date: "2024-01-02", State: "ready", Result: result}}, "market")
	if e != nil {
		t.Fatal(e)
	}
	row := readSeriesCSV(t, out)[0]
	if row["twr_index_numerator"] != "400" || row["twr_index_denominator"] != "3" || row["twr_index_decimal"] != "133.333333333333" || row["external_flow_numerator"] != "0" || row["daily_profit_numerator"] != "1" {
		t.Fatal(row)
	}
}
