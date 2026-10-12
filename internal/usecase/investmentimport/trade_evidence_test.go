package investmentimport

import (
	"strings"
	"testing"
	"time"
)

func evidenceFixture(t *testing.T) (string, FinancialMapping) {
	csv := financialHeader + "BUY,2024-01-01 12:00:00,TEST,10.123456789,3,RUB,0.01,X,0.125,,False,\n"
	mapping := financialConfig(t, csv)
	mapping.Timezone = "UTC"
	accrued := "0.38"
	mapping.TradeEvidence = map[int]TradeEvidence{1: {SourceRowID: mapping.SourceSHA256 + ":1", BrokerAccountID: "account", BrokerOperationID: "operation", SnapshotSHA256: strings.Repeat("a", 64), ReportHash: strings.Repeat("b", 64), InstrumentUID: "11111111-1111-4111-8111-111111111111", InstrumentType: "bond", Ticker: "TEST", Currency: "RUB", OperationType: "OPERATION_TYPE_BUY", State: "OPERATION_STATE_EXECUTED", Quantity: "3", Price: "10.123456789", Payment: "-30.75", Commission: "-0.01", Accrued: &accrued, BrokerDate: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), Reason: "Confirmed source and broker components"}}
	return csv, mapping
}
func TestBrokerEvidenceUsesActualCleanAndTotalAccrued(t *testing.T) {
	csv, m := evidenceFixture(t)
	r, e := MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || r.Ready != 1 {
		t.Fatal(r, e)
	}
	op := r.Rows[0].Operation
	if op.Amount != "30.37" || op.Accrued != "0.38" || op.Fee != "0.01" || op.Quantity != "3" || !op.SettlementAssumed || op.InstrumentKey != "tinvest:11111111-1111-4111-8111-111111111111" {
		t.Fatal(op)
	}
}
func TestBrokerEvidenceRejectsWrongFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*TradeEvidence)
	}{
		{"payment", func(e *TradeEvidence) { e.Payment = "-30.74" }},
		{"sign", func(e *TradeEvidence) { e.Payment = "30.75" }},
		{"missing accrued", func(e *TradeEvidence) { e.Accrued = nil }},
		{"source identity", func(e *TradeEvidence) { e.SourceRowID = "other:1" }},
		{"time", func(e *TradeEvidence) { e.BrokerDate = e.BrokerDate.Add(time.Hour) }},
		{"state", func(e *TradeEvidence) { e.State = "OPERATION_STATE_CANCELED" }},
		{"fee", func(e *TradeEvidence) { e.Commission = "-0.02" }},
		{"currency", func(e *TradeEvidence) { e.Currency = "USD" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			csv, m := evidenceFixture(t)
			v := m.TradeEvidence[1]
			test.change(&v)
			m.TradeEvidence[1] = v
			r, e := MapSnowballFinancial(strings.NewReader(csv), m)
			if e != nil || r.Ready != 0 || r.Rows[0].Operation != nil {
				t.Fatal(r, e)
			}
		})
	}
}
func TestBrokerEvidenceCannotReuseOneBrokerOperation(t *testing.T) {
	csv, m := evidenceFixture(t)
	csv += "SELL,2024-01-02 12:00:00,TEST,10.123456789,3,RUB,0.01,X,0.125,,False,\n"
	preview, err := PreviewSnowball(strings.NewReader(csv), true)
	if err != nil {
		t.Fatal(err)
	}
	m.SourceSHA256 = preview.SourceSHA256
	v := m.TradeEvidence[1]
	v.SourceRowID = preview.SourceSHA256 + ":1"
	m.TradeEvidence[1] = v
	v.SourceRowID = preview.SourceSHA256 + ":2"
	m.TradeEvidence[2] = v
	m.LedgerByRecord[2] = m.LedgerByRecord[1]
	if _, err = MapSnowballFinancial(strings.NewReader(csv), m); err == nil {
		t.Fatal("one broker execution used twice")
	}
}

func TestBrokerEvidencePreservesDateOnlyAndExplicitSettlement(t *testing.T) {
	csv, m := evidenceFixture(t)
	csv = strings.ReplaceAll(csv, "12:00:00", "00:00:00")
	p, e := PreviewSnowball(strings.NewReader(csv), true)
	if e != nil {
		t.Fatal(e)
	}
	m.SourceSHA256 = p.SourceSHA256
	v := m.TradeEvidence[1]
	v.SourceRowID = p.SourceSHA256 + ":1"
	v.DateOnly = true
	m.TradeEvidence[1] = v
	r, e := MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || r.Ready != 1 || r.Rows[0].Operation.OccurredAt.Hour() != 0 || r.Rows[0].Operation.Note == "" || r.Rows[0].Operation.Timing.OccurredDate == "" {
		t.Fatal(r, e)
	}
	m.SettlementPolicy = "explicit"
	r, e = MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || r.Ready != 0 {
		t.Fatal("invented settlement", r, e)
	}
	m.SettlementByRecord = map[int]time.Time{1: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)}
	r, e = MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || r.Ready != 1 || r.Rows[0].Operation.SettlementAssumed {
		t.Fatal(r, e)
	}
}

func TestBrokerEvidenceDoesNotRoundAwayFeePrecision(t *testing.T) {
	csv, m := evidenceFixture(t)
	csv = strings.Replace(csv, ",0.01,X,", ",0.0000000000001,X,", 1)
	preview, e := PreviewSnowball(strings.NewReader(csv), true)
	if e != nil {
		t.Fatal(e)
	}
	m.SourceSHA256 = preview.SourceSHA256
	v := m.TradeEvidence[1]
	v.SourceRowID = preview.SourceSHA256 + ":1"
	v.Commission = "-0.0000000000001"
	m.TradeEvidence[1] = v
	result, e := MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || result.Ready != 0 {
		t.Fatal("fee precision silently lost", result, e)
	}
}

func TestRoundedAveragePriceRequiresExactIndependentCash(t *testing.T) {
	csv := financialHeader + "BUY,2025-05-27 13:55:13,TEST,3.46090625,1600,RUB,16.61,X,0,,False,\n"
	m := financialConfig(t, csv)
	m.Timezone = "UTC"
	proof := TradeEvidence{SourceRowID: m.SourceSHA256 + ":1", BrokerAccountID: "a", BrokerOperationID: "b", SnapshotSHA256: strings.Repeat("a", 64), ReportHash: strings.Repeat("b", 64), InstrumentUID: "11111111-1111-4111-8111-111111111111", InstrumentType: "share", Ticker: "TEST", Currency: "RUB", OperationType: "OPERATION_TYPE_BUY", State: "OPERATION_STATE_EXECUTED", Quantity: "1600", Price: "3.460906", Payment: "-5537.45", Commission: "-16.61", BrokerDate: time.Date(2025, 5, 27, 13, 55, 13, 0, time.UTC), Reason: "Rounded broker average and exact cash"}
	m.TradeEvidence = map[int]TradeEvidence{1: proof}
	report, err := MapSnowballFinancial(strings.NewReader(csv), m)
	if err != nil || report.Ready != 1 || report.Rows[0].Operation.Amount != "5537.45" {
		t.Fatal(report, err)
	}
	for _, change := range []func(*TradeEvidence){func(e *TradeEvidence) { e.Payment = "-5537.46" }, func(e *TradeEvidence) { e.Price = "3.460905" }, func(e *TradeEvidence) { e.InstrumentType = "bond" }} {
		bad := proof
		change(&bad)
		m.TradeEvidence[1] = bad
		report, err = MapSnowballFinancial(strings.NewReader(csv), m)
		if err != nil || report.Ready != 0 {
			t.Fatal(report, err)
		}
	}
}

func TestBothAveragePricesDeriveFromActualCash(t *testing.T) {
	csv := financialHeader + "BUY,2025-05-27 13:55:13,TEST,531.40833333,12,RUB,2.55,X,0,,False,\n"
	m := financialConfig(t, csv)
	m.Timezone = "UTC"
	proof := TradeEvidence{SourceRowID: m.SourceSHA256 + ":1", BrokerAccountID: "a", BrokerOperationID: "b", SnapshotSHA256: strings.Repeat("a", 64), ReportHash: strings.Repeat("b", 64), InstrumentUID: "11111111-1111-4111-8111-111111111111", InstrumentType: "share", Ticker: "TEST", Currency: "RUB", OperationType: "OPERATION_TYPE_BUY", State: "OPERATION_STATE_EXECUTED", Quantity: "12", Price: "531.408333", Payment: "-6376.9", Commission: "-2.55", BrokerDate: time.Date(2025, 5, 27, 13, 55, 13, 0, time.UTC), Reason: "Rounded broker average and exact cash"}
	m.TradeEvidence = map[int]TradeEvidence{1: proof}
	report, err := MapSnowballFinancial(strings.NewReader(csv), m)
	if err != nil || report.Ready != 1 || report.Rows[0].Operation.Amount != "6376.9" {
		t.Fatal(report, err)
	}
	for _, change := range []func(*TradeEvidence){func(e *TradeEvidence) { e.Payment = "-5537.46" }, func(e *TradeEvidence) { e.Price = "3.460905" }, func(e *TradeEvidence) { e.InstrumentType = "bond" }} {
		bad := proof
		change(&bad)
		m.TradeEvidence[1] = bad
		report, err = MapSnowballFinancial(strings.NewReader(csv), m)
		if err != nil || report.Ready != 0 {
			t.Fatal(report, err)
		}
	}
}

func TestSingleExecutionRestoresDateOnlyTime(t *testing.T) {
	csv, m := evidenceFixture(t)
	csv = strings.Replace(csv, "12:00:00", "00:00:00", 1)
	p := financialConfig(t, csv)
	m.SourceSHA256 = p.SourceSHA256
	e := m.TradeEvidence[1]
	e.SourceRowID = m.SourceSHA256 + ":1"
	e.DateOnly = true
	e.UseExecutionTime = true
	e.Execution = &TradeExecutionEvidence{ID: "trade", Date: e.BrokerDate.Add(time.Second), Quantity: e.Quantity, Price: e.Price}
	m.TradeEvidence[1] = e
	r, err := MapSnowballFinancial(strings.NewReader(csv), m)
	if err != nil || r.Ready != 1 || !r.Rows[0].Operation.OccurredAt.Equal(e.Execution.Date) || !r.Rows[0].Operation.SettlementAssumed || r.Rows[0].Operation.Timing.OccurredDate != "" || r.Rows[0].Operation.Timing.SourceDate == "" {
		t.Fatal(r, err)
	}
	for _, change := range []func(*TradeExecutionEvidence){func(x *TradeExecutionEvidence) { x.Quantity = "2" }, func(x *TradeExecutionEvidence) { x.Price = "1" }, func(x *TradeExecutionEvidence) { x.Date = x.Date.Add(24 * time.Hour) }, func(x *TradeExecutionEvidence) { x.Date = x.Date.Add(-time.Hour) }} {
		bad := *e.Execution
		change(&bad)
		changed := e
		changed.Execution = &bad
		m.TradeEvidence[1] = changed
		r, err = MapSnowballFinancial(strings.NewReader(csv), m)
		if err != nil || r.Ready != 0 {
			t.Fatal(r, err)
		}
	}
}

func TestSimultaneousExecutionsMustBeCompleteAndUnique(t *testing.T) {
	csv, m := evidenceFixture(t)
	csv = strings.Replace(csv, "12:00:00", "00:00:00", 1)
	updated := financialConfig(t, csv)
	m.SourceSHA256 = updated.SourceSHA256
	e := m.TradeEvidence[1]
	e.SourceRowID = m.SourceSHA256 + ":1"
	e.DateOnly = true
	e.UseExecutionTime = true
	e.Executions = []TradeExecutionEvidence{{ID: "a", Date: e.BrokerDate, Quantity: "1", Price: "10.123456787"}, {ID: "b", Date: e.BrokerDate, Quantity: "2", Price: "10.123456790"}}
	m.TradeEvidence[1] = e
	r, err := MapSnowballFinancial(strings.NewReader(csv), m)
	if err != nil || r.Ready != 1 || !r.Rows[0].Operation.OccurredAt.Equal(e.BrokerDate) {
		t.Fatal(r, err)
	}
	for _, change := range []func([]TradeExecutionEvidence){func(x []TradeExecutionEvidence) { x[1].ID = x[0].ID }, func(x []TradeExecutionEvidence) { x[1].Date = x[1].Date.Add(time.Nanosecond) }, func(x []TradeExecutionEvidence) { x[1].Quantity = "1" }, func(x []TradeExecutionEvidence) { x[1].Price = "10" }} {
		bad := e
		bad.Executions = append([]TradeExecutionEvidence(nil), e.Executions...)
		change(bad.Executions)
		m.TradeEvidence[1] = bad
		r, err = MapSnowballFinancial(strings.NewReader(csv), m)
		if err != nil || r.Ready != 0 {
			t.Fatal(r, err)
		}
	}
}
