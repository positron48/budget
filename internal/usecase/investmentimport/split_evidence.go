package investmentimport

import (
	"fmt"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"math/big"
	"strings"
)

// SplitEvidence confirms one source row, one account and an exact quantity
// ratio. It never authorizes a distribution/replacement or tax-cost allocation.
type SplitEvidence struct {
	BrokerAccountID string `json:"broker_account_id,omitempty"`
	InstrumentKey   string `json:"instrument_key"`
	NewUnits        string `json:"new_units"`
	OldUnits        string `json:"old_units"`
	Reference       string `json:"reference"`
	Reason          string `json:"reason"`
}

func evidenceSplit(row PreviewRow, e SplitEvidence, ledger string, m FinancialMapping) (d.InvestmentOperation, error) {
	invalid := func(code string) (d.InvestmentOperation, error) {
		return d.InvestmentOperation{}, fmt.Errorf("%s", code)
	}
	if row.Event != "SPLIT" || strings.TrimSpace(e.InstrumentKey) == "" || strings.TrimSpace(e.Reference) == "" || len(e.Reference) > 2000 || strings.TrimSpace(e.Reason) == "" || len(e.Reason) > 2000 {
		return invalid("split_evidence_required")
	}
	if !m.DateOnlyByRecord[row.Record] {
		return invalid("split_civil_date_required")
	}
	if m.SettlementDateByRecord[row.Record] != "" || !m.SettlementByRecord[row.Record].IsZero() {
		return invalid("split_settlement_not_supported")
	}
	// Parse before constructing a Rat: invalid integer input must never panic.
	parse := func(raw string) (*big.Rat, bool) {
		if len(raw) > 24 {
			return nil, false
		}
		n, ok := new(big.Int).SetString(raw, 10)
		if !ok || n.Sign() <= 0 || n.String() != raw {
			return nil, false
		}
		return new(big.Rat).SetInt(n), true
	}
	numerator, nok := parse(e.NewUnits)
	denominator, dok := parse(e.OldUnits)
	if !nok || !dok {
		return invalid("split_exact_ratio_required")
	}
	source, err := evidenceDecimal(row.Values["Price"])
	if err != nil || source.Sign() <= 0 || new(big.Rat).Quo(numerator, denominator).Cmp(source) != 0 {
		return invalid("split_source_ratio_mismatch")
	}
	for _, field := range []string{"Quantity", "FeeTax", "NKD"} {
		value := row.Values[field]
		if value == "" {
			value = "0"
		}
		n, err := evidenceDecimal(value)
		if err != nil || n.Sign() != 0 {
			return invalid("split_unallocated_money_or_quantity")
		}
	}
	op := d.InvestmentOperation{LedgerID: ledger, Kind: "split", InstrumentKey: e.InstrumentKey, Timing: mappedTiming(row, m), SplitNew: e.NewUnits, SplitOld: e.OldUnits, Note: e.Reference}
	// Export clock values on corporate-action rows are not execution times.
	// The explicitly reviewed civil date is authoritative for this mapping.
	if err = inv.ValidateOperation(&op); err != nil {
		return invalid("split_operation_invalid")
	}
	return op, nil
}
