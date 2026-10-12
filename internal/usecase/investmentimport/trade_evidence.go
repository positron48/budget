package investmentimport

import (
	"encoding/hex"
	"fmt"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"math/big"
	"strings"
	"time"
)

// TradeEvidence contains reviewed snapshot facts, not a provider signature.
// The source and arithmetic are checked again; hashes retain provenance.
type TradeExecutionEvidence struct {
	ID       string    `json:"id"`
	Date     time.Time `json:"date"`
	Quantity string    `json:"quantity"`
	Price    string    `json:"price"`
}
type TradeEvidence struct {
	Executions        []TradeExecutionEvidence `json:"executions,omitempty"`
	Execution         *TradeExecutionEvidence  `json:"execution,omitempty"`
	UseExecutionTime  bool                     `json:"use_execution_time,omitempty"`
	FIGI              string                   `json:"figi,omitempty"`
	PositionUID       string                   `json:"position_uid,omitempty"`
	SourceRowID       string                   `json:"source_row_id"`
	BrokerAccountID   string                   `json:"broker_account_id"`
	BrokerOperationID string                   `json:"broker_operation_id"`
	SnapshotSHA256    string                   `json:"snapshot_sha256"`
	ReportHash        string                   `json:"report_hash"`
	InstrumentUID     string                   `json:"instrument_uid"`
	InstrumentType    string                   `json:"instrument_type"`
	Ticker            string                   `json:"ticker"`
	Currency          string                   `json:"currency"`
	OperationType     string                   `json:"operation_type"`
	State             string                   `json:"state"`
	Quantity          string                   `json:"quantity"`
	Price             string                   `json:"price"`
	Payment           string                   `json:"payment"`
	Commission        string                   `json:"commission"`
	Accrued           *string                  `json:"accrued"`
	BrokerDate        time.Time                `json:"broker_date"`
	DateOnly          bool                     `json:"date_only"`
	Reason            string                   `json:"reason"`
}

func validEvidenceHash(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && strings.ToLower(value) == value
}
func evidenceDecimal(value string) (*big.Rat, error) {
	normalized, err := NormalizeSourceDecimal(value)
	if err != nil {
		return nil, err
	}
	v, ok := new(big.Rat).SetString(normalized)
	if !ok {
		return nil, fmt.Errorf("invalid decimal")
	}
	return v, nil
}
func evidenceTrade(row PreviewRow, e TradeEvidence, ledger string, occurred time.Time, location *time.Location, mapping FinancialMapping) (d.InvestmentOperation, error) {
	var empty d.InvestmentOperation
	invalid := func() (d.InvestmentOperation, error) { return empty, fmt.Errorf("broker_trade_evidence_invalid") }
	if e.SourceRowID != row.SourceRowID || !validEvidenceHash(e.SnapshotSHA256) || !validEvidenceHash(e.ReportHash) || strings.TrimSpace(e.Reason) == "" || len(e.Reason) > 2000 || len(e.BrokerAccountID) == 0 || len(e.BrokerAccountID) > 200 || len(e.BrokerOperationID) == 0 || len(e.BrokerOperationID) > 200 || strings.ContainsAny(e.BrokerAccountID+e.BrokerOperationID, "\r\n\t") {
		return invalid()
	}
	uid, err := uuid.Parse(e.InstrumentUID)
	if err != nil || uid.String() != e.InstrumentUID {
		return invalid()
	}
	if e.State != "OPERATION_STATE_EXECUTED" || e.Currency != row.Currency || e.Ticker != row.Instrument.Symbol || e.Currency != "RUB" && e.Currency != "USD" && e.Currency != "EUR" {
		return invalid()
	}
	if row.Event != "BUY" && row.Event != "SELL" || row.Event == "BUY" && e.OperationType != "OPERATION_TYPE_BUY" && e.OperationType != "OPERATION_TYPE_BUY_CARD" || row.Event == "SELL" && e.OperationType != "OPERATION_TYPE_SELL" {
		return invalid()
	}
	if e.InstrumentType != "share" && e.InstrumentType != "etf" && e.InstrumentType != "bond" {
		return invalid()
	}
	brokerWall := e.BrokerDate.In(location).Format("2006-01-02 15:04:05")
	if e.BrokerDate.IsZero() || !e.DateOnly && brokerWall != row.WallTime || e.DateOnly && (!strings.HasSuffix(row.WallTime, "00:00:00") || brokerWall[:10] != row.WallTime[:10]) {
		return invalid()
	}
	values := []string{e.Quantity, e.Price, e.Payment, e.Commission, row.Values["Quantity"], row.Values["Price"], row.Values["FeeTax"]}
	numbers := make([]*big.Rat, len(values))
	for i, value := range values {
		v, err := evidenceDecimal(value)
		if err != nil {
			return invalid()
		}
		numbers[i] = v
	}
	quantity, price, payment, commission, sourceQuantity, sourcePrice, sourceFee := numbers[0], numbers[1], numbers[2], numbers[3], numbers[4], numbers[5], numbers[6]
	if quantity.Sign() <= 0 || price.Sign() <= 0 || quantity.Cmp(sourceQuantity) != 0 || commission.Sign() > 0 || sourceFee.Sign() < 0 || new(big.Rat).Neg(commission).Cmp(sourceFee) != 0 || row.FeeCurrency != "" && row.FeeCurrency != e.Currency {
		return invalid()
	}
	if price.Cmp(sourcePrice) != 0 {
		normalized, _ := NormalizeSourceDecimal(row.Values["Price"])
		scale := 0
		if i := strings.IndexByte(normalized, '.'); i >= 0 {
			scale = len(normalized) - i - 1
		}
		gross := new(big.Rat).Abs(payment)
		cents, _ := new(big.Rat).SetString(gross.FloatString(2))
		average := new(big.Rat).Quo(gross, quantity)
		quote, _ := new(big.Rat).SetString(average.FloatString(6))
		sourceRounded, _ := new(big.Rat).SetString(average.FloatString(scale))
		if (e.InstrumentType != "share" && e.InstrumentType != "etf") || sourcePrice.Sign() <= 0 || scale < 6 || scale > 12 || cents.Cmp(gross) != 0 || quote.Cmp(price) != 0 || sourceRounded.Cmp(sourcePrice) != 0 {
			return invalid()
		}
	}
	if row.Event == "BUY" && payment.Sign() >= 0 || row.Event == "SELL" && payment.Sign() <= 0 {
		return invalid()
	}
	nkdText := row.Values["NKD"]
	if nkdText == "" {
		nkdText = "0"
	}
	nkd, err := evidenceDecimal(nkdText)
	if err != nil || nkd.Sign() < 0 {
		return invalid()
	}
	accrued := new(big.Rat)
	if e.Accrued != nil {
		accrued, err = evidenceDecimal(*e.Accrued)
		if err != nil || accrued.Sign() < 0 {
			return invalid()
		}
	}
	if e.InstrumentType == "bond" {
		if e.Accrued == nil {
			return empty, fmt.Errorf("broker_accrued_required")
		}
		rounded, _ := new(big.Rat).SetString(new(big.Rat).Mul(nkd, quantity).FloatString(2))
		if accrued.Cmp(rounded) != 0 {
			return invalid()
		}
	} else if accrued.Sign() != 0 || nkd.Sign() != 0 {
		return invalid()
	}
	gross := new(big.Rat).Abs(payment)
	calculated := new(big.Rat).Mul(new(big.Rat).Add(sourcePrice, nkd), quantity)
	rounded, _ := new(big.Rat).SetString(calculated.FloatString(2))
	if gross.Cmp(calculated) != 0 && gross.Cmp(rounded) != 0 {
		return empty, fmt.Errorf("broker_cash_mismatch")
	}
	principal := new(big.Rat).Sub(gross, accrued)
	if principal.Sign() <= 0 {
		return invalid()
	}
	// MoneyValue has at most nine fractional digits. Never round an imported
	// amount merely to make it fit the journal's precision.
	decimal := func(value *big.Rat) (string, error) {
		s := value.FloatString(12)
		v, _ := new(big.Rat).SetString(s)
		if v.Cmp(value) != 0 {
			return "", fmt.Errorf("precision loss")
		}
		return NormalizeSourceDecimal(s)
	}
	amount, err := decimal(principal)
	if err != nil {
		return invalid()
	}
	fullAccrued, err := decimal(accrued)
	if err != nil {
		return invalid()
	}
	fee, err := decimal(sourceFee)
	if err != nil {
		return invalid()
	}
	key, err := mapping.instrumentKey(e.InstrumentUID, e.FIGI, e.PositionUID)
	if err != nil {
		return empty, err
	}
	if e.UseExecutionTime {
		executions := e.Executions
		if e.Execution != nil {
			if len(executions) > 0 {
				return invalid()
			}
			executions = []TradeExecutionEvidence{*e.Execution}
		}
		if !e.DateOnly || len(executions) == 0 || len(executions) > 10000 {
			return invalid()
		}
		total := new(big.Rat)
		value := new(big.Rat)
		seen := map[string]bool{}
		at := executions[0].Date
		for _, execution := range executions {
			if strings.TrimSpace(execution.ID) == "" || len(execution.ID) > 200 || seen[execution.ID] || execution.Date.IsZero() || !execution.Date.Equal(at) || execution.Date.In(location).Format("2006-01-02") != row.WallTime[:10] || execution.Date.Before(e.BrokerDate) {
				return invalid()
			}
			seen[execution.ID] = true
			eq, err := evidenceDecimal(execution.Quantity)
			if err != nil || eq.Sign() <= 0 {
				return invalid()
			}
			total.Add(total, eq)
			ep, err := evidenceDecimal(execution.Price)
			if err != nil || ep.Sign() <= 0 {
				return invalid()
			}
			value.Add(value, new(big.Rat).Mul(eq, ep))
		}
		if total.Cmp(quantity) != 0 || value.Cmp(new(big.Rat).Mul(quantity, price)) != 0 {
			return invalid()
		}
		occurred = at
	}
	op := d.InvestmentOperation{Timing: mappedTiming(row, mapping), LedgerID: ledger, Kind: strings.ToLower(row.Event), InstrumentKey: key, Currency: e.Currency, Quantity: e.Quantity, Amount: amount, Accrued: fullAccrued, Fee: fee, OccurredAt: occurred}
	if e.DateOnly {
		op.Timing.SourceDate = row.WallTime[:10]
		if !e.UseExecutionTime {
			op.Timing.OccurredDate = row.WallTime[:10]
		} else {
			op.Timing.OccurredDate = ""
		}
	}
	if e.UseExecutionTime {
		op.Note = "Source date-only precision: occurrence time restored from verified simultaneous broker executions; settlement remains unconfirmed."
	} else if e.DateOnly {
		op.Note = "Source date-only precision: original midnight retained; broker order date does not confirm execution or settlement time."
	}
	if mapping.SettlementPolicy == "explicit" {
		op.SettlementAt = mapping.SettlementByRecord[row.Record]
		if op.SettlementAt.IsZero() && mapping.SettlementDateByRecord[row.Record] == "" {
			return empty, fmt.Errorf("settlement_time_required")
		}
	}
	applyMappedDateOnly(&op, row)
	if err = inv.ValidateOperation(&op); err != nil {
		return empty, fmt.Errorf("financial_operation_invalid")
	}
	return op, nil
}
