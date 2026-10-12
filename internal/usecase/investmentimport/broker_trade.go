package investmentimport

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

type brokerMoney struct {
	Currency, Units string
	Nano            int64
}

func (m *brokerMoney) value(currency string) (*big.Rat, error) {
	if m == nil || strings.ToUpper(m.Currency) != currency {
		return nil, fmt.Errorf("broker money missing or currency mismatch")
	}
	units, err := strconv.ParseInt(m.Units, 10, 64)
	if err != nil || m.Nano <= -1000000000 || m.Nano >= 1000000000 || units > 0 && m.Nano < 0 || units < 0 && m.Nano > 0 {
		return nil, fmt.Errorf("invalid broker money")
	}
	return new(big.Rat).Add(new(big.Rat).SetInt64(units), new(big.Rat).SetFrac64(m.Nano, 1000000000)), nil
}

type brokerTrade struct {
	ID, State, Type, InstrumentType, InstrumentUID, QuantityDone string
	AccountID                                                    string `json:"brokerAccountId"`
	ParentID                                                     string `json:"parentOperationId"`
	Payment, Commission, AccruedInt                              *brokerMoney
	Date                                                         time.Time
	TradesInfo                                                   struct {
		Trades []struct {
			Num, Quantity string
			Date          time.Time
		}
	}
}
type BrokerReplayCheck struct {
	LedgerVersion int64
	Valid         bool
	Issue         string
}

type BrokerTradeCandidate struct {
	PostingAllowed   bool
	Replay           BrokerReplayCheck
	JournalMatches   []BrokerJournalMatch
	Operation        d.InvestmentOperation
	SourceIdentities []string
	SourceHashes     []string
	SnapshotSHA256   string
}

// PrepareBrokerTrade requires complete simultaneous execution times and a
// separately identified fee. The caller must reserve every source identity
// atomically with posting and reconcile already posted Snowball equivalents.
func PrepareBrokerTrade(snapshot BrokerSnapshot, operationID, ledgerID string) (BrokerTradeCandidate, error) {
	var out BrokerTradeCandidate
	invalid := func(reason string) (BrokerTradeCandidate, error) {
		return BrokerTradeCandidate{}, fmt.Errorf("broker trade: %s", reason)
	}
	if id, err := uuid.Parse(ledgerID); err != nil || id.String() != ledgerID {
		return invalid("ledger required")
	}
	verified, verifyErr := PrepareBrokerSnapshot(snapshot.Original)
	if verifyErr != nil {
		return invalid("verified snapshot required")
	}
	snapshot = verified
	var primary *BrokerSourceRow
	var trade brokerTrade
	for i := range snapshot.Rows {
		if snapshot.Rows[i].OperationID == operationID {
			primary = &snapshot.Rows[i]
			break
		}
	}
	if primary == nil {
		return invalid("source operation not found")
	}
	if err := json.Unmarshal(primary.Raw, &trade); err != nil {
		return invalid("invalid source operation")
	}
	kind := map[string]string{"OPERATION_TYPE_BUY": "buy", "OPERATION_TYPE_BUY_CARD": "buy", "OPERATION_TYPE_SELL": "sell", "OPERATION_TYPE_SELL_CARD": "sell"}[trade.Type]
	if kind == "" || trade.State != "OPERATION_STATE_EXECUTED" || trade.AccountID != snapshot.AccountID || trade.ID != primary.OperationID {
		return invalid("executed trade required")
	}
	if trade.InstrumentType != "share" && trade.InstrumentType != "etf" && trade.InstrumentType != "bond" {
		return invalid("unsupported instrument")
	}
	if id, err := uuid.Parse(trade.InstrumentUID); err != nil || id.String() != trade.InstrumentUID {
		return invalid("instrument UID required")
	}
	if trade.Payment == nil {
		return invalid("payment missing")
	}
	currency := strings.ToUpper(trade.Payment.Currency)
	payment, err := trade.Payment.value(currency)
	if err != nil {
		return invalid(err.Error())
	}
	fee, err := trade.Commission.value(currency)
	if err != nil || fee.Sign() > 0 {
		return invalid("commission required")
	}
	if kind == "buy" && payment.Sign() >= 0 || kind == "sell" && payment.Sign() <= 0 {
		return invalid("payment sign")
	}
	accrued := new(big.Rat)
	if trade.AccruedInt != nil {
		accrued, err = trade.AccruedInt.value(currency)
		if err != nil || accrued.Sign() < 0 {
			return invalid("invalid accrued interest")
		}
	}
	if trade.InstrumentType == "bond" && trade.AccruedInt == nil || trade.InstrumentType != "bond" && accrued.Sign() != 0 {
		return invalid("accrued interest evidence required")
	}
	quantity, err := evidenceDecimal(trade.QuantityDone)
	if err != nil || quantity.Sign() <= 0 {
		return invalid("positive completed quantity required")
	}
	executions := trade.TradesInfo.Trades
	if len(executions) == 0 || len(executions) > 10000 {
		return invalid("execution times required")
	}
	at := executions[0].Date
	sum := new(big.Rat)
	seen := map[string]bool{}
	for _, execution := range executions {
		if execution.Num == "" || seen[execution.Num] || at.IsZero() || !execution.Date.Equal(at) || at.Before(trade.Date) || at.Before(snapshot.From) || at.After(snapshot.To) {
			return invalid("complete simultaneous executions required")
		}
		seen[execution.Num] = true
		q, err := evidenceDecimal(execution.Quantity)
		if err != nil || q.Sign() <= 0 {
			return invalid("execution quantity")
		}
		sum.Add(sum, q)
	}
	if sum.Cmp(quantity) != 0 {
		return invalid("incomplete executions")
	}
	sources := []BrokerSourceRow{*primary}
	fees := 0
	for _, row := range snapshot.Rows {
		var op brokerTrade
		if err := json.Unmarshal(row.Raw, &op); err != nil {
			return invalid("invalid related operation")
		}
		if op.ParentID != trade.ID || op.AccountID != trade.AccountID || op.Type != "OPERATION_TYPE_BROKER_FEE" || op.State != "OPERATION_STATE_EXECUTED" {
			continue
		}
		actual, err := op.Payment.value(currency)
		if err != nil || actual.Cmp(fee) != 0 {
			return invalid("separate commission mismatch")
		}
		fees++
		sources = append(sources, row)
	}
	if fees > 1 || fee.Sign() < 0 && fees != 1 {
		return invalid("unique separate commission required")
	}
	principal := new(big.Rat).Sub(new(big.Rat).Abs(payment), accrued)
	if principal.Sign() <= 0 {
		return invalid("positive clean principal required")
	}
	number := func(v *big.Rat) string { s, _ := NormalizeSourceDecimal(v.FloatString(12)); return s }
	out.Operation = d.InvestmentOperation{LedgerID: ledgerID, Kind: kind, InstrumentKey: "tinvest:" + trade.InstrumentUID, Currency: currency, Quantity: trade.QuantityDone, Amount: number(principal), Accrued: number(accrued), Fee: number(new(big.Rat).Neg(fee)), OccurredAt: at, Note: "Broker snapshot cash and complete simultaneous executions; settlement remains assumed."}
	if err := inv.ValidateOperation(&out.Operation); err != nil {
		return invalid(err.Error())
	}
	out.SnapshotSHA256 = snapshot.SHA256
	for _, source := range sources {
		out.SourceIdentities = append(out.SourceIdentities, source.Identity)
		out.SourceHashes = append(out.SourceHashes, source.SHA256)
	}
	return out, nil
}
