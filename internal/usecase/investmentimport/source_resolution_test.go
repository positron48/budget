package investmentimport

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

func resolutionFixture(t *testing.T) (string, FinancialMapping) {
	t.Helper()
	csv := financialHeader + "BUY,2024-01-01 12:00:00,TEST,10,3,RUB,0.01,X,0,,False,\n" +
		"SELL,2024-01-02 12:00:00,TEST,11,1,RUB,0,X,0,,False,\n" +
		"BUY,2024-01-03 12:00:00,UNKNOWN,10,1,RUB,0,X,1,,True,\n" +
		"DIVIDEND,2024-01-04 12:00:00,TEST,2,6,RUB,1,X,0,,False,\n" +
		"SPLIT,2024-01-05 12:00:00,TEST,2,0,RUB,0,X,0,,False,\n"
	m := financialConfig(t, csv)
	m.Timezone = "UTC"
	m.TradeEvidence = map[int]TradeEvidence{1: {
		SourceRowID: m.SourceSHA256 + ":1", BrokerAccountID: "account", BrokerOperationID: "operation",
		SnapshotSHA256: strings.Repeat("a", 64), ReportHash: strings.Repeat("b", 64),
		InstrumentUID: "11111111-1111-4111-8111-111111111111", InstrumentType: "share", Ticker: "TEST",
		Currency: "RUB", OperationType: "OPERATION_TYPE_BUY", State: "OPERATION_STATE_EXECUTED",
		Quantity: "3", Price: "10", Payment: "-30", Commission: "-0.01",
		BrokerDate: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), Reason: "Confirmed execution",
	}}
	delete(m.LedgerByRecord, 2)
	return csv, m
}

func TestSourceResolutionSuggestionsNeverPrepareOperations(t *testing.T) {
	csv, mapping := resolutionFixture(t)
	before, _ := json.Marshal(mapping)
	r, err := ReviewSourceResolution(strings.NewReader(csv), mapping)
	if err != nil || r.Prepared != 1 || r.Unresolved != 4 || len(r.Decisions) != 5 {
		t.Fatalf("counts: %+v, %v", r, err)
	}
	candidate := r.Decisions[1]
	if candidate.State != "review_required" || candidate.SuggestedInstrumentKey != "tinvest:11111111-1111-4111-8111-111111111111" || !slices.Equal(candidate.AnchorRecords, []int{1}) || !slices.Contains(candidate.Reviews, "historical_listing_identity_review_required") || !slices.Contains(candidate.Reviews, "residual_account_assignment_required") {
		t.Fatal(candidate)
	}
	for code, count := range map[string]int{
		"security_identity_evidence_required": 1, "historical_nominal_and_accrued_basis_required": 1,
		"cash_suppression_scope_required": 1, "income_price_metadata_review_required": 1,
		"corporate_event_identity_scope_and_effective_date_required": 1,
	} {
		if r.ReviewCounts[code] != count {
			t.Fatalf("%s: %d", code, r.ReviewCounts[code])
		}
	}
	after, _ := json.Marshal(mapping)
	if string(before) != string(after) {
		t.Fatal("input mapping mutated")
	}
	again, err := ReviewSourceResolution(strings.NewReader(csv), mapping)
	if err != nil || again.Hash != r.Hash || len(r.Hash) != 64 {
		t.Fatal("unstable review hash", err)
	}
	mapping.SourceSHA256 = strings.Repeat("0", 64)
	if _, err = ReviewSourceResolution(strings.NewReader(csv), mapping); err == nil {
		t.Fatal("different source accepted")
	}
}

func TestSourceResolutionRejectsUnusableIdentityAnchor(t *testing.T) {
	csv, mapping := resolutionFixture(t)
	v := mapping.TradeEvidence[1]
	v.InstrumentType = "bond"
	mapping.TradeEvidence[1] = v
	r, err := ReviewSourceResolution(strings.NewReader(csv), mapping)
	if err != nil {
		t.Fatal(err)
	}
	if r.Decisions[1].SuggestedInstrumentKey != "" {
		t.Fatal("bond units inferred from ticker", r.Decisions[1])
	}
}

func TestSourceResolutionConflictingCanonicalIdentitiesBlockSuggestion(t *testing.T) {
	csv, mapping := resolutionFixture(t)
	csv += "BUY,2024-01-06 12:00:00,TEST,12,1,RUB,0,X,0,,False,\n"
	preview, err := PreviewSnowball(strings.NewReader(csv), true)
	if err != nil {
		t.Fatal(err)
	}
	mapping.SourceSHA256 = preview.SourceSHA256
	v := mapping.TradeEvidence[1]
	v.SourceRowID = preview.SourceSHA256 + ":1"
	mapping.TradeEvidence[1] = v
	v.SourceRowID = preview.SourceSHA256 + ":2"
	v.InstrumentUID = "22222222-2222-4222-8222-222222222222"
	v.BrokerOperationID = "different-operation"
	v.OperationType = "OPERATION_TYPE_SELL"
	v.Quantity, v.Price, v.Payment, v.Commission = "1", "11", "11", "0"
	v.BrokerDate = v.BrokerDate.Add(24 * time.Hour)
	mapping.TradeEvidence[2] = v
	mapping.LedgerByRecord[2] = mapping.LedgerByRecord[1]
	r, err := ReviewSourceResolution(strings.NewReader(csv), mapping)
	if err != nil || r.Prepared != 2 {
		t.Fatal(r, err)
	}
	row := r.Decisions[5]
	if row.SuggestedInstrumentKey != "" || !slices.Contains(row.Reviews, "security_identity_or_unit_conflict") {
		t.Fatal("ambiguous historical identity suggested", row)
	}
}
