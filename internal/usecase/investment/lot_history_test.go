package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

func TestLotHistoryPreservesAncestryAndCutoff(t *testing.T) {
	ops := distributionHistory()
	got, e := ReplayLotHistory(testLedger(), ops, day.Add(24*time.Hour), []string{"old"})
	if e != nil {
		t.Fatal(e)
	}
	if len(got.LotHistory) != 4 {
		t.Fatalf("changes: %+v", got.LotHistory)
	}
	buy, source, target, sell := got.LotHistory[0], got.LotHistory[1], got.LotHistory[2], got.LotHistory[3]
	if buy.Before != nil || buy.After.CleanCost.RatString() != "20" || source.Before.CleanCost.RatString() != "20" || source.After.CleanCost.RatString() != "10" || target.Before != nil || target.After.CleanCost.RatString() != "10" || sell.After.CleanCost.RatString() != "5" || sell.Before.CleanCost.RatString() != "10" || !sell.After.AcquiredAt.Equal(ops[1].OccurredAt) {
		t.Fatal(got.LotHistory)
	}
	if target.InstrumentKey != "NEW" || source.InstrumentKey != "OLD" || sell.Kind != "sell" {
		t.Fatal(got.LotHistory)
	}
	// Historical pointers must not alias mutable book state or later snapshots.
	sell.After.CleanCost.SetInt64(99)
	if target.After.CleanCost.RatString() != "10" || got.ExactLots["NEW"][0].CleanCost.RatString() != "5" {
		t.Fatal("mutable trace alias")
	}
	prior, e := ReplayLotHistory(testLedger(), ops, ops[2].OccurredAt, []string{"old"})
	if e != nil || len(prior.LotHistory) != 1 || prior.ExactLots["OLD"][0].CleanCost.RatString() != "20" {
		t.Fatal(prior, e)
	}
	normal, e := Replay(testLedger(), ops, day.Add(24*time.Hour))
	if e != nil || len(normal.LotHistory) != 0 {
		t.Fatal(normal, e)
	}
	ops[4].Quantity = "14.027"
	disposed, e := ReplayLotHistory(testLedger(), ops, day.Add(24*time.Hour), []string{"old"})
	if e != nil || disposed.LotHistory[3].After != nil {
		t.Fatal(disposed, e)
	}
}
func TestLotHistorySplitAndExactPartialSale(t *testing.T) {
	cash := op("cash", "deposit", "100", 1)
	buy := op("buy", "buy", "1", 2)
	buy.Quantity = "3"
	buy.InstrumentKey = "S"
	sell := op("sell", "sell", "1", 3)
	sell.Quantity = "1"
	sell.InstrumentKey = "S"
	split := op("split", "split", "0", 4)
	split.InstrumentKey = "S"
	split.Currency = ""
	split.SplitNew = "2"
	split.SplitOld = "1"
	v, e := ReplayLotHistory(testLedger(), []d.InvestmentOperation{cash, buy, sell, split}, day.Add(24*time.Hour), []string{"buy"})
	if e != nil {
		t.Fatal(e)
	}
	if len(v.LotHistory) != 3 || v.LotHistory[1].After.CleanCost.RatString() != "2/3" || v.LotHistory[2].After.Quantity.RatString() != "4" || v.LotHistory[2].After.CleanCost.RatString() != "2/3" {
		t.Fatal(v.LotHistory)
	}
}
