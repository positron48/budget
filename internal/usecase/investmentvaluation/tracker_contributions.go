package investmentvaluation

import "math/big"

type TrackerContributionValue struct{ Quantity, Value *big.Rat }
type TrackerContribution struct {
	InScope                                                          bool
	LedgerID, LedgerName, InstrumentKey, GroupID, GroupName, Reason  string
	Opening, Closing, Flow, Profit, OpeningQuantity, ClosingQuantity *big.Rat
}
