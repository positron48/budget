package investmentimport

import (
	"strings"
	"testing"
	"time"
)

func incomeFixture(t *testing.T) (string, FinancialMapping) {
	csv := financialHeader + "DIVIDEND,2024-01-01 15:00:00,TEST,0,100.123456789,RUB,13,X,0,,False,\n"
	m := financialConfig(t, csv)
	credit := IncomeBrokerOperation{ID: "credit", AccountID: "account", Type: "OPERATION_TYPE_DIVIDEND", State: "OPERATION_STATE_EXECUTED", Ticker: "TEST", Currency: "RUB", Payment: "100.123456789", Date: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}
	tax := credit
	tax.ID = "tax"
	tax.Type = "OPERATION_TYPE_DIVIDEND_TAX"
	tax.Payment = "-13"
	m.IncomeEvidence = map[int]IncomeEvidence{1: {SourceRowID: m.SourceSHA256 + ":1", SnapshotSHA256: strings.Repeat("a", 64), ReportHash: strings.Repeat("b", 64), InstrumentUID: "11111111-1111-4111-8111-111111111111", Reason: "Reviewed gross credit and separate withholding", Credit: credit, Tax: tax}}
	return csv, m
}
func TestIncomeEvidenceGrossAndCoupon(t *testing.T) {
	for _, coupon := range []bool{false, true} {
		csv, m := incomeFixture(t)
		e := m.IncomeEvidence[1]
		kind := "dividend"
		if coupon {
			kind = "coupon"
			e.Credit.Type = "OPERATION_TYPE_COUPON"
			e.Tax.Type = "OPERATION_TYPE_BOND_TAX"
			m.IncomeEvidence[1] = e
		}
		r, err := MapSnowballFinancial(strings.NewReader(csv), m)
		if err != nil || r.Ready != 1 {
			t.Fatal(r, err)
		}
		op := r.Rows[0].Operation
		if op.Kind != kind || op.Amount != "100.123456789" || op.Tax != "13" || op.CouponAllocationConfirmed {
			t.Fatal(op)
		}
	}
}
func TestIncomeEvidenceRejectsChangedProof(t *testing.T) {
	changes := map[string]func(*IncomeEvidence){
		"account":  func(e *IncomeEvidence) { e.Tax.AccountID = "other" },
		"currency": func(e *IncomeEvidence) { e.Tax.Currency = "USD" },
		"time":     func(e *IncomeEvidence) { e.Tax.Date = e.Tax.Date.Add(time.Second) },
		"sign":     func(e *IncomeEvidence) { e.Tax.Payment = "13" },
		"cash":     func(e *IncomeEvidence) { e.Credit.Payment = "101" },
		"tax":      func(e *IncomeEvidence) { e.Tax.Payment = "-12" },
		"state":    func(e *IncomeEvidence) { e.Tax.State = "OPERATION_STATE_CANCELED" },
		"type":     func(e *IncomeEvidence) { e.Tax.Type = "OPERATION_TYPE_BOND_TAX" },
		"source":   func(e *IncomeEvidence) { e.SourceRowID = "other:1" },
		"identity": func(e *IncomeEvidence) { e.InstrumentUID = "bad" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			csv, m := incomeFixture(t)
			e := m.IncomeEvidence[1]
			change(&e)
			m.IncomeEvidence[1] = e
			r, err := MapSnowballFinancial(strings.NewReader(csv), m)
			if err != nil || r.Ready != 0 || r.Unresolved != 1 {
				t.Fatal(r, err)
			}
		})
	}
}
func TestIncomeEvidenceCannotReuseBrokerOperation(t *testing.T) {
	csv, m := incomeFixture(t)
	e := m.IncomeEvidence[1]
	e.Tax.ID = e.Credit.ID
	m.IncomeEvidence[1] = e
	if _, err := MapSnowballFinancial(strings.NewReader(csv), m); err == nil {
		t.Fatal("shared credit/tax ID accepted")
	}
	csv, m = incomeFixture(t)
	m.TradeEvidence = map[int]TradeEvidence{1: {BrokerAccountID: "account", BrokerOperationID: "different"}}
	if _, err := MapSnowballFinancial(strings.NewReader(csv), m); err == nil {
		t.Fatal("two evidence kinds accepted for one row")
	}
}

func TestIncomeEvidenceObservedPaymentKeepsTaxReview(t *testing.T) {
	csv, m := incomeFixture(t)
	csv = strings.Replace(csv, ",RUB,13,X", ",RUB,0,X", 1)
	updated := financialConfig(t, csv)
	e := m.IncomeEvidence[1]
	e.SourceRowID = updated.SourceSHA256 + ":1"
	e.TaxReviewRequired = true
	e.Tax = IncomeBrokerOperation{}
	updated.IncomeEvidence = map[int]IncomeEvidence{1: e}
	r, err := MapSnowballFinancial(strings.NewReader(csv), updated)
	if err != nil || r.Ready != 1 || !r.Rows[0].Operation.TaxReviewRequired || r.Rows[0].Operation.Tax != "0" {
		t.Fatal(r, err)
	}
	e.TaxReviewRequired = false
	updated.IncomeEvidence[1] = e
	r, err = MapSnowballFinancial(strings.NewReader(csv), updated)
	if err != nil || r.Ready != 0 {
		t.Fatal("missing tax accepted without review flag", r, err)
	}
}

func TestIncomePriceMetadataRequiresExactExplicitRetention(t *testing.T) {
	csv, m := incomeFixture(t)
	csv = strings.Replace(csv, ",TEST,0,", ",TEST,94.52,", 1)
	updated := financialConfig(t, csv)
	e := m.IncomeEvidence[1]
	e.SourceRowID = updated.SourceSHA256 + ":1"
	updated.IncomeEvidence = map[int]IncomeEvidence{1: e}
	r, err := MapSnowballFinancial(strings.NewReader(csv), updated)
	if err != nil || r.Ready != 0 {
		t.Fatal("unreviewed price accepted", r, err)
	}
	e.SourcePriceMetadata = "94.52"
	updated.IncomeEvidence[1] = e
	r, err = MapSnowballFinancial(strings.NewReader(csv), updated)
	if err != nil || r.Ready != 1 || r.Rows[0].Operation.Amount != "100.123456789" || !strings.Contains(r.Rows[0].Operation.Note, "94.52") {
		t.Fatal(r, err)
	}
	e.SourcePriceMetadata = "94.53"
	updated.IncomeEvidence[1] = e
	r, err = MapSnowballFinancial(strings.NewReader(csv), updated)
	if err != nil || r.Ready != 0 {
		t.Fatal("metadata mismatch", r, err)
	}
}
