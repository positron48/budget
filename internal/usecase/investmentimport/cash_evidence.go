package investmentimport

import (
	"fmt"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"math/big"
	"strings"
	"time"
)

type CashEvidence struct {
	FIGI           string                `json:"figi,omitempty"`
	PositionUID    string                `json:"position_uid,omitempty"`
	SourceRowID    string                `json:"source_row_id"`
	SnapshotSHA256 string                `json:"snapshot_sha256"`
	ReportHash     string                `json:"report_hash"`
	Reason         string                `json:"reason"`
	InstrumentUID  string                `json:"instrument_uid"`
	InstrumentType string                `json:"instrument_type"`
	Operation      IncomeBrokerOperation `json:"operation"`
}

func evidenceCash(row PreviewRow, e CashEvidence, ledger string, occurred time.Time, location *time.Location, mapping FinancialMapping) (d.InvestmentOperation, error) {
	var empty d.InvestmentOperation
	invalid := func() (d.InvestmentOperation, error) { return empty, fmt.Errorf("broker_cash_evidence_invalid") }
	b := e.Operation
	if e.SourceRowID != row.SourceRowID || !validEvidenceHash(e.SnapshotSHA256) || !validEvidenceHash(e.ReportHash) || strings.TrimSpace(e.Reason) == "" || len(e.Reason) > 2000 || len(b.AccountID) == 0 || len(b.AccountID) > 200 || len(b.ID) == 0 || len(b.ID) > 200 || strings.ContainsAny(b.AccountID+b.ID, "\x00\r\n\t") {
		return invalid()
	}
	if b.State != "OPERATION_STATE_EXECUTED" || b.Currency != row.Currency || b.Date.IsZero() || b.Date.In(location).Format("2006-01-02 15:04:05") != row.WallTime || row.FeeCurrency != "" && row.FeeCurrency != row.Currency {
		return invalid()
	}
	if row.Instrument.Symbol != "" && row.Instrument.Symbol != row.Currency {
		return invalid()
	}
	kind, instrument, basis, sign := "", "", "Quantity", 1
	switch row.Event {
	case "CASH_IN":
		if b.Type != "OPERATION_TYPE_INPUT" {
			return invalid()
		}
		kind = "deposit"
	case "TAX":
		if b.Type != "OPERATION_TYPE_TAX" {
			return invalid()
		}
		kind = "tax"
		basis = "FeeTax"
		sign = -1
	case "CASH_GAIN":
		if b.Type != "OPERATION_TYPE_OVERNIGHT" || b.Ticker == "" || (e.InstrumentType != "share" && e.InstrumentType != "etf" && e.InstrumentType != "bond") {
			return invalid()
		}
		uid, err := uuid.Parse(e.InstrumentUID)
		if err != nil || uid.String() != e.InstrumentUID {
			return invalid()
		}
		kind = "repo_income"
		instrument, err = mapping.instrumentKey(e.InstrumentUID, e.FIGI, e.PositionUID)
		if err != nil {
			return empty, err
		}
	default:
		return invalid()
	}
	if kind != "repo_income" && (b.Ticker != "" || e.InstrumentUID != "" || e.InstrumentType != "") {
		return invalid()
	}
	values := map[string]*big.Rat{}
	for _, key := range []string{"Quantity", "Price", "FeeTax", "NKD"} {
		v := row.Values[key]
		if v == "" {
			v = "0"
		}
		n, err := evidenceDecimal(v)
		if err != nil || n.Sign() < 0 {
			return invalid()
		}
		values[key] = n
	}
	payment, err := evidenceDecimal(b.Payment)
	if err != nil {
		return invalid()
	}
	expected := values[basis]
	if expected.Sign() <= 0 || payment.Cmp(new(big.Rat).Mul(expected, big.NewRat(int64(sign), 1))) != 0 || values["NKD"].Sign() != 0 {
		return invalid()
	}
	if kind == "tax" {
		if values["Quantity"].Sign() != 0 || values["Price"].Sign() != 0 {
			return invalid()
		}
	} else if values["Price"].Cmp(big.NewRat(1, 1)) != 0 || values["FeeTax"].Sign() != 0 {
		return invalid()
	}
	// This describes only the observed payment. Zero source tax is not tax exemption.
	op := d.InvestmentOperation{Timing: mappedTiming(row, mapping), LedgerID: ledger, Kind: kind, InstrumentKey: instrument, Currency: row.Currency, Amount: row.Values[basis], OccurredAt: occurred}
	applyMappedDateOnly(&op, row)
	if err := inv.ValidateOperation(&op); err != nil {
		return invalid()
	}
	return op, nil
}
