package investmentimport

import (
	"strings"
	"testing"
	"time"
)

func TestCashEvidenceTypesAndRejectsChanges(t *testing.T) {
	for _, event := range []string{"CASH_IN", "TAX", "CASH_GAIN"} {
		price, quantity, fee, payment, typ, kind := "1", "2.123456789", "0", "2.123456789", "OPERATION_TYPE_INPUT", "deposit"
		if event == "TAX" {
			price, quantity, fee, payment, typ, kind = "0", "0", "13", "-13", "OPERATION_TYPE_TAX", "tax"
		}
		if event == "CASH_GAIN" {
			typ, kind = "OPERATION_TYPE_OVERNIGHT", "repo_income"
		}
		csv := financialHeader + event + ",2024-01-01 15:00:00,RUB," + price + "," + quantity + ",RUB," + fee + ",,0,,False,\n"
		m := financialConfig(t, csv)
		e := CashEvidence{SourceRowID: m.SourceSHA256 + ":1", SnapshotSHA256: strings.Repeat("a", 64), ReportHash: strings.Repeat("b", 64), Reason: "Reviewed cash", Operation: IncomeBrokerOperation{ID: "op", AccountID: "a", Type: typ, State: "OPERATION_STATE_EXECUTED", Currency: "RUB", Payment: payment, Date: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}}
		if event == "CASH_GAIN" {
			e.InstrumentUID = "11111111-1111-4111-8111-111111111111"
			e.InstrumentType = "share"
			e.Operation.Ticker = "CBOM"
		}
		m.CashEvidence = map[int]CashEvidence{1: e}
		r, err := MapSnowballFinancial(strings.NewReader(csv), m)
		if err != nil || r.Ready != 1 || r.Rows[0].Operation.Kind != kind {
			t.Fatal(event, r, err)
		}
		if event == "CASH_GAIN" && r.Rows[0].Operation.InstrumentKey != "tinvest:"+e.InstrumentUID {
			t.Fatal(r)
		}
		for _, change := range []func(*CashEvidence){func(e *CashEvidence) { e.Operation.Payment = "999" }, func(e *CashEvidence) { e.Operation.Currency = "USD" }, func(e *CashEvidence) { e.Operation.Date = e.Operation.Date.Add(time.Second) }, func(e *CashEvidence) { e.SourceRowID = "wrong" }} {
			bad := e
			change(&bad)
			m.CashEvidence[1] = bad
			r, err = MapSnowballFinancial(strings.NewReader(csv), m)
			if err != nil || r.Ready != 0 {
				t.Fatal(event, r, err)
			}
		}
	}
}
