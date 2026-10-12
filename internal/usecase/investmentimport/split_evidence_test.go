package investmentimport

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFinancialSplitEvidenceExactAndSourceBound(t *testing.T) {
	for _, ratio := range []struct{ price, new, old string }{{"10", "10", "1"}, {"0,0002", "1", "5000"}} {
		csv := financialHeader + "SPLIT,2024-07-14 03:00:00,S," + "\"" + ratio.price + "\"" + ",0,RUB,0,X,0,,,\n"
		m := financialConfig(t, csv)
		m.DateOnlyByRecord = map[int]bool{1: true}
		m.SplitEvidence = map[int]SplitEvidence{1: {InstrumentKey: "S@X", NewUnits: ratio.new, OldUnits: ratio.old, Reference: "Issuer terms checked against source", Reason: "Quantity ratio and original civil date verified"}}
		r, e := MapSnowballFinancial(strings.NewReader(csv), m)
		if e != nil || r.Ready != 1 || r.Rows[0].Operation == nil {
			t.Fatal(r, e)
		}
		op := r.Rows[0].Operation
		if op.Kind != "split" || op.Amount != "0" || op.Quantity != "0" || op.SplitNew != ratio.new || op.SplitOld != ratio.old || op.Timing.OccurredDate != "2024-07-14" || op.Timing.SourceDate != "2024-07-14" {
			t.Fatal(op)
		}
		before := r.Hash
		v := m.SplitEvidence[1]
		v.Reference = "Different explicit evidence"
		m.SplitEvidence[1] = v
		r, e = MapSnowballFinancial(strings.NewReader(csv), m)
		if e != nil || r.Hash == before {
			t.Fatal("evidence not hash-bound", e)
		}
	}
}
func TestFinancialSplitRejectsAmbiguityAndUnallocatedFacts(t *testing.T) {
	base := financialHeader + "SPLIT,2024-07-14 03:00:00,S,10,0,RUB,0,X,0,,,\n"
	for _, tc := range []struct {
		name, code string
		change     func(*FinancialMapping)
	}{
		{"date", "split_civil_date_required", func(m *FinancialMapping) { m.DateOnlyByRecord = nil }},
		{"ratio", "split_source_ratio_mismatch", func(m *FinancialMapping) { v := m.SplitEvidence[1]; v.NewUnits = "11"; m.SplitEvidence[1] = v }},
		{"invalid", "split_exact_ratio_required", func(m *FinancialMapping) { v := m.SplitEvidence[1]; v.OldUnits = "bad"; m.SplitEvidence[1] = v }},
		{"settlement", "split_settlement_not_supported", func(m *FinancialMapping) { m.SettlementByRecord = map[int]time.Time{1: time.Now()} }},
		{"reference", "split_evidence_required", func(m *FinancialMapping) { v := m.SplitEvidence[1]; v.Reference = " "; m.SplitEvidence[1] = v }},
		{"ambiguous", "ambiguous_financial_evidence", func(m *FinancialMapping) {
			m.Rules = []FinancialRule{{Event: "SPLIT", Symbol: "S", Exchange: "X", Currency: "RUB", Kind: "split", AmountFactor: "1", Reason: "Another rule"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := financialConfig(t, base)
			m.DateOnlyByRecord = map[int]bool{1: true}
			m.SplitEvidence = map[int]SplitEvidence{1: {InstrumentKey: "S@X", NewUnits: "10", OldUnits: "1", Reference: "Issuer", Reason: "Verified"}}
			tc.change(&m)
			r, e := MapSnowballFinancial(strings.NewReader(base), m)
			if e != nil || r.Ready != 0 || !slices.Contains(r.Rows[0].Issues, tc.code) {
				t.Fatal(r, e)
			}
		})
	}
	for _, suffix := range []string{"10,1,RUB,0,X,0,,,", "10,0,RUB,1,X,0,,,", "10,0,RUB,0,X,1,,,"} {
		csv := financialHeader + "SPLIT,2024-07-14 03:00:00,S," + suffix + "\n"
		m := financialConfig(t, csv)
		m.DateOnlyByRecord = map[int]bool{1: true}
		m.SplitEvidence = map[int]SplitEvidence{1: {InstrumentKey: "S@X", NewUnits: "10", OldUnits: "1", Reference: "Issuer", Reason: "Verified"}}
		r, e := MapSnowballFinancial(strings.NewReader(csv), m)
		if e != nil || r.Ready != 0 || !slices.Contains(r.Rows[0].Issues, "split_unallocated_money_or_quantity") {
			t.Fatal(r, e)
		}
	}
}
func TestFinancialSplitDuplicateRowsStillRequireReview(t *testing.T) {
	row := "SPLIT,2024-07-14 03:00:00,S,10,0,RUB,0,X,0,,,\n"
	csv := financialHeader + row + row
	m := financialConfig(t, csv)
	m.DateOnlyByRecord = map[int]bool{1: true, 2: true}
	m.SplitEvidence = map[int]SplitEvidence{}
	for _, record := range []int{1, 2} {
		m.SplitEvidence[record] = SplitEvidence{InstrumentKey: "S@X", NewUnits: "10", OldUnits: "1", Reference: "Issuer", Reason: "Verified"}
	}
	r, e := MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || r.Ready != 0 || !slices.Contains(r.Rows[0].Issues, "identical_rows_require_review") {
		t.Fatal(r, e)
	}
}

func TestFinancialSplitCivilDateIgnoresPlaceholderClock(t *testing.T) {
	csv := financialHeader + "SPLIT,2024-03-31 02:30:00,S,10,0,RUB,0,X,0,,,\n"
	m := financialConfig(t, csv)
	m.Timezone = "Europe/Berlin"
	m.DateOnlyByRecord = map[int]bool{1: true}
	m.SplitEvidence = map[int]SplitEvidence{1: {InstrumentKey: "S@X", NewUnits: "10", OldUnits: "1", Reference: "Issuer terms", Reason: "Only the date is known"}}
	r, e := MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || r.Ready != 1 || r.Rows[0].Operation.OccurredAt.Format(time.RFC3339) != "2024-03-30T23:00:00Z" {
		t.Fatal(r, e)
	}
}
