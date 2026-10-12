package investmentimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"slices"
)

// Preview is an immutable description of source interpretation, not a ledger.
// Missing settlement/timezone and cash coverage are file-wide prerequisites.
type Preview struct {
	Version      int            `json:"version"`
	SourceSHA256 string         `json:"source_sha256"`
	Hash         string         `json:"hash"`
	Scope        string         `json:"scope"`
	State        string         `json:"state"`
	Requirements []string       `json:"requirements"`
	Rows         []PreviewRow   `json:"rows"`
	IssueCounts  map[string]int `json:"issue_counts"`
}

type PreviewRow struct {
	SourceRowID        string            `json:"source_row_id"`
	Record             int               `json:"record"`
	Event              string            `json:"event"`
	WallTime           string            `json:"wall_time"`
	Instrument         SourceInstrument  `json:"instrument"`
	Currency           string            `json:"currency"`
	FeeCurrency        string            `json:"fee_currency"`
	Values             map[string]string `json:"values"`
	SuppressCash       bool              `json:"suppress_cash"`
	CandidatePrincipal string            `json:"candidate_principal,omitempty"`
	IdenticalRecords   []int             `json:"identical_records,omitempty"`
	Issues             []string          `json:"issues"`
}

// PreviewSnowball re-parses original bytes rather than trusting mutable staged
// fields. No implicit account, currency, event identity, or price basis is chosen.
func PreviewSnowball(input io.Reader, combined bool) (Preview, error) {
	source, err := StageSnowball(input, combined)
	if err != nil {
		return Preview{}, err
	}
	p := Preview{Version: 1, SourceSHA256: source.Audit.SHA256, Scope: source.Scope,
		State: "requires_mapping", Requirements: []string{"timezone_required", "settlement_policy_required", "cash_reconciliation_required"},
		Rows: make([]PreviewRow, 0, len(source.Rows)), IssueCounts: map[string]int{}}
	groups := map[string][]int{}
	for _, row := range source.Rows {
		b, _ := json.Marshal(row.Raw)
		groups[string(b)] = append(groups[string(b)], row.Record)
	}
	for _, row := range source.Rows {
		r := PreviewRow{SourceRowID: row.ID, Record: row.Record, Event: row.Raw["Event"], WallTime: row.Raw["Date"],
			Instrument: SourceInstrument{Symbol: row.Raw["Symbol"], Exchange: row.Raw["Exchange"]},
			Currency:   row.Raw["Currency"], FeeCurrency: row.Raw["FeeCurrency"], Values: row.Decimals,
			SuppressCash: row.Raw["DoNotAdjustCash"] == "True", Issues: []string{}}
		issue := func(code string) { r.Issues = append(r.Issues, code) }
		b, _ := json.Marshal(row.Raw)
		if records := groups[string(b)]; len(records) > 1 {
			r.IdenticalRecords = slices.Clone(records)
			issue("identical_rows_require_review")
		}
		for _, key := range []string{"Price", "Quantity", "FeeTax", "NKD"} {
			if v := row.Decimals[key]; v != "" && decimalSign(v) < 0 {
				issue("negative_" + key)
			}
		}
		if decimalSign(row.Decimals["FeeTax"]) != 0 && r.FeeCurrency == "" {
			issue("fee_currency_required")
		}
		switch r.Event {
		case "BUY", "SELL":
			issue("instrument_mapping_required")
			issue("price_basis_required")
			if decimalSign(row.Decimals["Quantity"]) <= 0 {
				issue("positive_quantity_required")
			}
			if decimalSign(row.Decimals["Price"]) <= 0 {
				issue("positive_price_required")
			}
			if decimalSign(row.Decimals["NKD"]) != 0 {
				issue("accrued_interest_basis_required")
			}
			// Only an illustrative quantity*price, never settlement cash. Bond
			// prices may be percent of nominal; fees and NKD remain separate.
			r.CandidatePrincipal = exactProduct(row.Decimals["Quantity"], row.Decimals["Price"])
		case "SPLIT":
			issue("instrument_mapping_required")
			issue("corporate_action_scope_required")
			issue("split_ratio_semantics_required")
		case "DIVIDEND", "REPAYMENT":
			issue("instrument_mapping_required")
			issue("income_or_redemption_semantics_required")
		case "CASH_IN", "CASH_GAIN", "FEE", "TAX":
			issue("cash_event_semantics_required")
		default:
			issue("unsupported_event")
		}
		if r.SuppressCash {
			issue("cash_suppression_requires_review")
		}
		for _, code := range r.Issues {
			p.IssueCounts[code]++
		}
		p.Rows = append(p.Rows, r)
	}
	b, err := json.Marshal(p)
	if err != nil {
		return Preview{}, err
	}
	h := sha256.Sum256(b)
	p.Hash = hex.EncodeToString(h[:])
	return p, nil
}

func decimalSign(v string) int {
	n, ok := new(big.Rat).SetString(v)
	if !ok {
		return 0
	} // Only called on validated source decimals or absent NKD.
	return n.Sign()
}

func exactProduct(a, b string) string {
	x, _ := new(big.Rat).SetString(a)
	y, _ := new(big.Rat).SetString(b)
	if x == nil || y == nil {
		return ""
	}
	scale := func(v string) int {
		for i := range v {
			if v[i] == '.' {
				return len(v) - i - 1
			}
		}
		return 0
	}
	value := x.Mul(x, y).FloatString(scale(a) + scale(b))
	result, _ := NormalizeSourceDecimal(value)
	// Products may exceed the source parser's input length limit.
	if result == "" {
		return value
	}
	return result
}
