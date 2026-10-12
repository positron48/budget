package investmentimport

import (
	d "github.com/positron48/budget/internal/domain"
	"time"
)

type BrokerJournalMatch struct {
	OperationVersion                                      int64
	OperationID, LedgerID, InstrumentKey                  string
	SameInstrument, SameLedger, ExactTime, SnowballSource bool
}

// PossibleBrokerTradeMatches is a review aid, never proof of uniqueness.
// It includes other ledgers and instrument aliases because historical Snowball
// assignments and corporate-action symbols may differ from the broker source.
func PossibleBrokerTradeMatches(candidate d.InvestmentOperation, operations []d.InvestmentOperation) []BrokerJournalMatch {
	matches := []BrokerJournalMatch{}
	equal := func(a, b string) bool {
		x, err := evidenceDecimal(a)
		if err != nil {
			return false
		}
		y, err := evidenceDecimal(b)
		return err == nil && x.Cmp(y) == 0
	}
	for _, operation := range operations {
		delta := operation.OccurredAt.Sub(candidate.OccurredAt)
		if operation.Status != "posted" || operation.Kind != candidate.Kind || operation.Currency != candidate.Currency || delta < -24*time.Hour || delta > 24*time.Hour {
			continue
		}
		if !equal(operation.Quantity, candidate.Quantity) || !equal(operation.Amount, candidate.Amount) || !equal(operation.Fee, candidate.Fee) || !equal(operation.Accrued, candidate.Accrued) || !equal(operation.Tax, candidate.Tax) {
			continue
		}
		matches = append(matches, BrokerJournalMatch{OperationVersion: operation.Version, OperationID: operation.ID, LedgerID: operation.LedgerID, InstrumentKey: operation.InstrumentKey, SameInstrument: operation.InstrumentKey == candidate.InstrumentKey, SameLedger: operation.LedgerID == candidate.LedgerID, ExactTime: operation.OccurredAt.Equal(candidate.OccurredAt), SnowballSource: operation.Source != nil})
	}
	return matches
}
