package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"slices"
	"testing"
	"time"
)

func distributionHistory() []d.InvestmentOperation {
	deposit := op("cash", "deposit", "100", 1)
	old := op("old", "buy", "20", 2)
	old.Quantity = "2"
	old.InstrumentKey = "OLD"
	recent := op("recent", "buy", "10", 3)
	recent.Quantity = "1"
	recent.InstrumentKey = "NEW"
	action := op("action", "distribution", "0", 4)
	action.InstrumentKey = "OLD"
	action.Note = "confirmed terms"
	action.Distribution = &d.InvestmentDistribution{EventKey: "event", TargetInstrumentKey: "NEW", NewUnits: "7.0135", OldUnits: "1", CostNumerator: "1", CostDenominator: "2"}
	sell := op("sell", "sell", "15", 5)
	sell.Quantity = "7.0135"
	sell.InstrumentKey = "NEW"
	return []d.InvestmentOperation{deposit, old, recent, action, sell}
}
func TestReplayDistributionPreservesCostAndAcquisitionOrder(t *testing.T) {
	ops := distributionHistory()
	result, err := Replay(testLedger(), ops, day.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.Cash[0].Economic != "85" || len(result.SecurityLegs) != 4 {
		t.Fatal(result)
	}
	newPos, oldPos := result.Snapshot.Positions[0], result.Snapshot.Positions[1]
	if newPos.InstrumentKey != "NEW" || newPos.Quantity != "8.0135" || newPos.CleanCost != "15" || newPos.RealizedClean != "10" || oldPos.Quantity != "2" || oldPos.CleanCost != "10" {
		t.Fatal(newPos, oldPos)
	}
	if newPos.Lots[0].OriginID != "old" || !newPos.Lots[0].AcquiredAt.Equal(ops[1].OccurredAt) {
		t.Fatal(newPos.Lots)
	}
	if result.ExactLots["NEW"][0].OriginID != "old" || !result.ExactLots["NEW"][0].AcquiredAt.Equal(ops[1].OccurredAt) || result.ExactLots["NEW"][0].CleanCost.RatString() != "5" {
		t.Fatal(result.ExactLots)
	}
	// The input terms are immutable even though validation normalizes a copy.
	if ops[3].Distribution.CostNumerator != "1" {
		t.Fatal(ops[3])
	}
	before, err := Replay(testLedger(), ops, ops[2].OccurredAt)
	if err != nil || before.Snapshot.Positions[0].Quantity != "1" || before.Snapshot.Positions[1].CleanCost != "20" {
		t.Fatal(before, err)
	}
}
func TestReplayDistributionRejectsDuplicateAndInvalidHistory(t *testing.T) {
	for _, change := range []func([]d.InvestmentOperation){
		func(x []d.InvestmentOperation) { x[3].Distribution.TargetInstrumentKey = "OLD" },
		func(x []d.InvestmentOperation) { x[3].Distribution.CostNumerator = "3" },
		func(x []d.InvestmentOperation) { x[3].Distribution.ConsumeSource = true },
		func(x []d.InvestmentOperation) { x[3].Distribution.OldUnits = "0" },
		func(x []d.InvestmentOperation) { x[3].Distribution.OldUnits = "3" },
		func(x []d.InvestmentOperation) { x[3].Currency = "RUB" },
		func(x []d.InvestmentOperation) { x[3].Amount = "1" },
		func(x []d.InvestmentOperation) { x[3].Note = "" },
		func(x []d.InvestmentOperation) { x[1].Accrued = "1" },
	} {
		ops := distributionHistory()
		change(ops)
		if _, err := Replay(testLedger(), ops, day.Add(24*time.Hour)); err == nil {
			t.Fatal("invalid distribution accepted", ops[3])
		}
	}
	ops := distributionHistory()
	duplicate := ops[3]
	duplicate.ID = "duplicate"
	duplicate.Sequence = 6
	duplicate.OccurredAt = day.Add(6 * time.Hour)
	if _, err := Replay(testLedger(), append(ops, duplicate), day.Add(24*time.Hour)); err == nil {
		t.Fatal("duplicate event accepted")
	}
}
func TestReplayReplacementCarriesUnknownCost(t *testing.T) {
	opening := op("opening", "opening_position", "0", 1)
	opening.OccurredAt = day
	opening.InstrumentKey = "OLD"
	opening.Quantity = "2"
	opening.OpeningAccruedKnown = true
	opening.AcquiredAt = day.Add(-24 * time.Hour)
	opening.Note = "unknown clean cost"
	action := distributionHistory()[3]
	action.Distribution.ConsumeSource = true
	action.Distribution.CostDenominator = "1"
	result, err := Replay(testLedger(), []d.InvestmentOperation{opening, action}, day.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.Positions[0].CleanCost != "" || result.Snapshot.Positions[1].Quantity != "0" || !slices.Contains(result.Snapshot.Issues, "unknown_acquisition_cost") || len(result.CashLegs) != 0 {
		t.Fatal(result)
	}
	if len(result.SecurityLegs) != 3 {
		t.Fatal(result.SecurityLegs)
	}
}
