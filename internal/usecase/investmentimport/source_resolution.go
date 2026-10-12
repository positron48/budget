package investmentimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"sort"
)

// SourceResolution is a read-only inventory, not executable financial mapping.
// A matching ticker/exchange is only an identity suggestion: historical listing
// continuity and corporate-action scope still require independent review.
type SourceResolution struct {
	Version                                 int
	SourceSHA256, FinancialReviewHash, Hash string
	Rows, Prepared, Unresolved              int
	Decisions                               []SourceResolutionDecision
	ReviewCounts                            map[string]int
}
type SourceResolutionDecision struct {
	Record                    int
	SourceRowID, Event, State string
	Reviews                   []string
	SuggestedInstrumentKey    string `json:",omitempty"`
	AnchorRecords             []int  `json:",omitempty"`
}
type sourceIdentity struct{ symbol, exchange, currency string }
type sourceIdentityAnchor struct {
	key     string
	records []int
	blocked bool
}

func ReviewSourceResolution(input io.Reader, mapping FinancialMapping) (SourceResolution, error) {
	var result SourceResolution
	raw, e := io.ReadAll(io.LimitReader(input, MaxSourceBytes+1))
	if e != nil || len(raw) > MaxSourceBytes {
		return result, fmt.Errorf("invalid source size")
	}
	preview, e := PreviewSnowball(bytes.NewReader(raw), true)
	if e != nil {
		return result, e
	}
	financial, e := MapSnowballFinancial(bytes.NewReader(raw), mapping)
	if e != nil {
		return result, e
	}
	result = SourceResolution{Version: 1, SourceSHA256: financial.SourceSHA256, FinancialReviewHash: financial.Hash, Rows: len(preview.Rows), Prepared: financial.Ready, Unresolved: financial.Unresolved, Decisions: []SourceResolutionDecision{}, ReviewCounts: map[string]int{}}
	anchors := map[sourceIdentity]*sourceIdentityAnchor{}
	for i, row := range preview.Rows {
		evidence, ok := mapping.TradeEvidence[row.Record]
		if !ok {
			continue
		}
		identity := sourceIdentity{row.Instrument.Symbol, row.Instrument.Exchange, row.Currency}
		a := anchors[identity]
		if a == nil {
			a = &sourceIdentityAnchor{}
			anchors[identity] = a
		}
		op := financial.Rows[i].Operation
		if op == nil || (evidence.InstrumentType != "share" && evidence.InstrumentType != "etf") || op.InstrumentKey == "" {
			a.blocked = true
			continue
		}
		q, ok := new(big.Rat).SetString(row.Values["Quantity"])
		units, valid := new(big.Rat).SetString(op.Quantity)
		if !ok || !valid || q.Cmp(units) != 0 || op.Accrued != "0" || (a.key != "" && a.key != op.InstrumentKey) {
			a.blocked = true
			continue
		}
		a.key = op.InstrumentKey
		a.records = append(a.records, row.Record)
	}
	for i, row := range preview.Rows {
		decision := SourceResolutionDecision{Record: row.Record, SourceRowID: row.SourceRowID, Event: row.Event, State: "prepared", Reviews: []string{}}
		if financial.Rows[i].Operation != nil {
			result.Decisions = append(result.Decisions, decision)
			continue
		}
		decision.State = "review_required"
		seen := map[string]bool{}
		review := func(code string) {
			if !seen[code] {
				seen[code] = true
				decision.Reviews = append(decision.Reviews, code)
				result.ReviewCounts[code]++
			}
		}
		for _, code := range financial.Rows[i].Issues {
			if code != "financial_rule_required" && code != "ledger_mapping_required" {
				review(code)
			}
		}
		if mapping.LedgerByRecord[row.Record] == "" {
			review("residual_account_assignment_required")
		}
		if row.SuppressCash {
			review("cash_suppression_scope_required")
		}
		switch row.Event {
		case "BUY", "SELL":
			a := anchors[sourceIdentity{row.Instrument.Symbol, row.Instrument.Exchange, row.Currency}]
			if a == nil || a.key == "" {
				review("security_identity_evidence_required")
			} else if a.blocked {
				review("security_identity_or_unit_conflict")
			} else {
				decision.SuggestedInstrumentKey = a.key
				decision.AnchorRecords = append([]int(nil), a.records...)
				sort.Ints(decision.AnchorRecords)
				review("historical_listing_identity_review_required")
			}
			review("trade_amount_and_settlement_review_required")
			if v := row.Values["NKD"]; v != "" && v != "0" {
				review("historical_nominal_and_accrued_basis_required")
			}
			if row.FeeCurrency != "" && row.FeeCurrency != row.Currency {
				review("cross_currency_fee_evidence_required")
			}
		case "DIVIDEND":
			review("income_kind_gross_net_and_withholding_review_required")
			if v := row.Values["Price"]; v != "" && v != "0" {
				review("income_price_metadata_review_required")
			}
		case "SPLIT":
			review("corporate_event_identity_scope_and_effective_date_required")
		case "REPAYMENT":
			review("principal_event_identity_nominal_and_cash_required")
		case "CASH_IN":
			review("cash_or_security_credit_semantics_required")
		case "FEE", "TAX":
			review("standalone_cash_event_and_component_deduplication_required")
		default:
			review("financial_semantics_evidence_required")
		}
		result.Decisions = append(result.Decisions, decision)
	}
	encoded, e := json.Marshal(result)
	if e != nil {
		return result, e
	}
	sum := sha256.Sum256(encoded)
	result.Hash = hex.EncodeToString(sum[:])
	return result, nil
}
