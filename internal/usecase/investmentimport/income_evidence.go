package investmentimport

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

// IncomeEvidence retains two distinct broker operations: gross credit and tax.
// Snapshot hashes provide provenance, not provider authentication.
type IncomeBrokerOperation struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	Type      string    `json:"type"`
	State     string    `json:"state"`
	Ticker    string    `json:"ticker"`
	Currency  string    `json:"currency"`
	Payment   string    `json:"payment"`
	Date      time.Time `json:"date"`
}
type IncomeEvidence struct {
	SourcePriceMetadata string                `json:"source_price_metadata,omitempty"`
	TaxReviewRequired   bool                  `json:"tax_review_required,omitempty"`
	FIGI                string                `json:"figi,omitempty"`
	PositionUID         string                `json:"position_uid,omitempty"`
	SourceRowID         string                `json:"source_row_id"`
	SnapshotSHA256      string                `json:"snapshot_sha256"`
	ReportHash          string                `json:"report_hash"`
	InstrumentUID       string                `json:"instrument_uid"`
	Reason              string                `json:"reason"`
	Credit              IncomeBrokerOperation `json:"credit"`
	Tax                 IncomeBrokerOperation `json:"tax"`
}

func evidenceIncome(row PreviewRow, e IncomeEvidence, ledger string, occurred time.Time, location *time.Location, mapping FinancialMapping) (d.InvestmentOperation, error) {
	var empty d.InvestmentOperation
	invalid := func() (d.InvestmentOperation, error) { return empty, fmt.Errorf("broker_income_evidence_invalid") }
	uid, err := uuid.Parse(e.InstrumentUID)
	if err != nil || uid.String() != e.InstrumentUID || row.Event != "DIVIDEND" || e.SourceRowID != row.SourceRowID || !validEvidenceHash(e.SnapshotSHA256) || !validEvidenceHash(e.ReportHash) || strings.TrimSpace(e.Reason) == "" || len(e.Reason) > 2000 {
		return invalid()
	}
	kind, taxType := "dividend", "OPERATION_TYPE_DIVIDEND_TAX"
	switch e.Credit.Type {
	case "OPERATION_TYPE_COUPON":
		kind, taxType = "coupon", "OPERATION_TYPE_BOND_TAX"
	case "OPERATION_TYPE_DIVIDEND":
	default:
		return invalid()
	}
	if !e.TaxReviewRequired && (e.Tax.Type != taxType || e.Credit.AccountID != e.Tax.AccountID || e.Credit.ID == e.Tax.ID) {
		return invalid()
	}
	operations := []IncomeBrokerOperation{e.Credit}
	if e.TaxReviewRequired {
		if e.Tax != (IncomeBrokerOperation{}) {
			return invalid()
		}
	} else {
		operations = append(operations, e.Tax)
	}
	for _, op := range operations {
		if len(op.AccountID) == 0 || len(op.AccountID) > 200 || len(op.ID) == 0 || len(op.ID) > 200 || strings.ContainsAny(op.AccountID+op.ID, "\x00\r\n\t") || op.State != "OPERATION_STATE_EXECUTED" || op.Ticker != row.Instrument.Symbol || op.Currency != row.Currency || op.Date.IsZero() || op.Date.In(location).Format("2006-01-02 15:04:05") != row.WallTime {
			return invalid()
		}
	}
	if row.Currency != "RUB" && row.Currency != "USD" && row.Currency != "EUR" || row.FeeCurrency != "" && row.FeeCurrency != row.Currency {
		return invalid()
	}
	taxPayment := e.Tax.Payment
	if e.TaxReviewRequired {
		taxPayment = "0"
	}
	values := []string{e.Credit.Payment, taxPayment, row.Values["Quantity"], row.Values["FeeTax"], row.Values["Price"], row.Values["NKD"]}
	numbers := make([]*big.Rat, len(values))
	for i, v := range values {
		if i == 5 && v == "" {
			v = "0"
		}
		n, err := evidenceDecimal(v)
		if err != nil {
			return invalid()
		}
		numbers[i] = n
	}
	credit, tax := numbers[0], numbers[1]
	if credit.Sign() <= 0 || (!e.TaxReviewRequired && tax.Sign() >= 0) || credit.Cmp(numbers[2]) != 0 || new(big.Rat).Neg(tax).Cmp(numbers[3]) != 0 || numbers[5].Sign() != 0 {
		return invalid()
	}
	if numbers[4].Sign() != 0 || e.SourcePriceMetadata != "" {
		metadata, err := evidenceDecimal(e.SourcePriceMetadata)
		if err != nil || metadata.Sign() < 0 || metadata.Cmp(numbers[4]) != 0 {
			return invalid()
		}
	}
	// Source precision is preserved; ValidateOperation rejects excess precision.
	key, err := mapping.instrumentKey(e.InstrumentUID, e.FIGI, e.PositionUID)
	if err != nil {
		return empty, err
	}
	op := d.InvestmentOperation{Timing: mappedTiming(row, mapping), TaxReviewRequired: e.TaxReviewRequired, LedgerID: ledger, Kind: kind, InstrumentKey: key, Currency: row.Currency, Amount: e.Credit.Payment, Tax: row.Values["FeeTax"], OccurredAt: occurred}
	if e.SourcePriceMetadata != "" {
		op.Note = "Source Price retained as uninterpreted metadata (" + e.SourcePriceMetadata + "); income amount is the verified broker cash credit, not a Price product."
	}
	applyMappedDateOnly(&op, row)
	if err := inv.ValidateOperation(&op); err != nil {
		return invalid()
	}
	return op, nil
}
