package investment

import (
	"math/big"
	"testing"

	d "github.com/positron48/budget/internal/domain"
)

func nativeHistory() (d.InvestmentLedger, []d.InvestmentOperation) {
	ledger := testLedger()
	ledger.PortfolioTracking = true
	rub := op("rub-buy", "buy", "200", 1)
	rub.InstrumentKey, rub.Currency, rub.Quantity, rub.Fee, rub.FeeCurrency = "BOND", "RUB", "2", "2", "USD"
	usd := op("usd-buy", "buy", "30", 2)
	usd.InstrumentKey, usd.Currency, usd.Quantity, usd.Fee, usd.FeeCurrency = "BOND", "USD", "3", "0.3", "RUB"
	split := op("split", "split", "0", 3)
	split.InstrumentKey, split.Currency, split.SplitNew, split.SplitOld = "BOND", "", "2", "1"
	sell := op("usd-sell", "sell", "60", 4)
	sell.InstrumentKey, sell.Currency, sell.Quantity = "BOND", "USD", "3"
	return ledger, []d.InvestmentOperation{rub, usd, split, sell}
}

func TestNativePortfolioFIFOAcrossCurrenciesAndSplit(t *testing.T) {
	ledger, ops := nativeHistory()
	result, e := Replay(ledger, ops, day.AddDate(0, 0, 1))
	if e != nil {
		t.Fatal(e)
	}
	p := result.Snapshot.Positions[0]
	if p.Quantity != "7" || p.CleanCost != "" || p.CostBasis != "principal" || len(p.Lots) != 2 || p.Lots[0].Currency != "RUB" || p.Lots[0].Quantity != "1" || p.Lots[0].CleanCost != "50" || p.Lots[1].Currency != "USD" || p.Lots[1].Quantity != "6" || p.Lots[1].CleanCost != "30" {
		t.Fatal(p)
	}
	if len(result.CashLegs) != 0 || len(result.Snapshot.Cash) != 0 || len(result.SecurityLegs) != 4 || result.SecurityLegs[2].Quantity != "5" {
		t.Fatal(result)
	}
	if result.ExactPositions["BOND"].CleanCost != nil || result.ExactLots["BOND"][1].Currency != "USD" {
		t.Fatal("mixed native money was summed", result)
	}
	fees := map[string]*big.Rat{}
	for _, event := range result.ProfitEvents {
		if event.Kind == "fees" {
			nativeAdd(fees, event.Currency, event.Amount)
		}
	}
	if fees["USD"].RatString() != "-2" || fees["RUB"].RatString() != "-3/10" {
		t.Fatal(fees)
	}
	// Sell the remaining RUB lot and part of the USD lot in a RUB transaction.
	sell := op("rub-sell", "sell", "240", 5)
	sell.InstrumentKey, sell.Currency, sell.Quantity = "BOND", "RUB", "2"
	result, e = Replay(ledger, append(ops, sell), day.AddDate(0, 0, 1))
	if e != nil {
		t.Fatal(e)
	}
	p = result.Snapshot.Positions[0]
	if p.Quantity != "5" || p.Currency != "USD" || p.CleanCost != "25" || p.RealizedClean != "" || len(p.Lots) != 1 {
		t.Fatal(p)
	}
}

func TestNativePortfolioFutureHistoryValidatedWithoutChangingPastSnapshot(t *testing.T) {
	ledger, ops := nativeHistory()
	at := ops[0].OccurredAt
	result, e := ReplayLotHistory(ledger, ops, at, []string{"rub-buy"})
	if e != nil || result.Snapshot.Positions[0].Quantity != "2" || result.Snapshot.Positions[0].CleanCost != "200" || len(result.LotHistory) != 1 {
		t.Fatal(result, e)
	}
	ops[3].Quantity = "11"
	if _, e = Replay(ledger, ops, at); e == nil {
		t.Fatal("invalid future sale accepted")
	}
}

func TestNativePortfolioUnknownOpeningBasisStaysUnknownAfterPartialSale(t *testing.T) {
	ledger := testLedger()
	ledger.PortfolioTracking = true
	opening := op("unknown-opening", "opening_position", "0", 1)
	opening.InstrumentKey, opening.Quantity, opening.Note = "STOCK", "2", "Synthetic incomplete acquisition history"
	sell := op("sell", "sell", "50", 2)
	sell.InstrumentKey, sell.Quantity = "STOCK", "1"
	result, e := Replay(ledger, []d.InvestmentOperation{opening, sell}, day.AddDate(0, 0, 1))
	if e != nil {
		t.Fatal(e)
	}
	p := result.Snapshot.Positions[0]
	if p.Quantity != "1" || p.CleanCost != "" || p.RealizedClean != "" || result.ExactLots["STOCK"][0].CleanCost != nil {
		t.Fatal(p)
	}
}

func TestNativeFeeDoesNotEnableCrossCurrencyLegacyCashOrShortSelling(t *testing.T) {
	ledger, ops := nativeHistory()
	ledger.PortfolioTracking = false
	if _, e := Replay(ledger, ops, day.AddDate(0, 0, 1)); e == nil {
		t.Fatal("native portfolio fee entered a cash book")
	}
	ledger.PortfolioTracking = true
	ops[3].Fee, ops[3].FeeCurrency = "200", "RUB" // Not comparable to USD proceeds.
	if _, e := Replay(ledger, ops, day.AddDate(0, 0, 1)); e != nil {
		t.Fatal(e)
	}
	ops[3].Quantity = "11"
	if _, e := Replay(ledger, ops, day.AddDate(0, 0, 1)); e == nil {
		t.Fatal("short sale accepted")
	}
	ops[3].Quantity = "3"
	ops[1].TenantID = "another-tenant"
	if _, e := Replay(ledger, ops, day.AddDate(0, 0, 1)); e == nil {
		t.Fatal("foreign operation accepted")
	}
}

func TestNativePortfolioTracePreservesOriginalCurrencyAndConsumedLot(t *testing.T) {
	ledger, ops := nativeHistory()
	result, e := ReplayLotHistory(ledger, ops, day.AddDate(0, 0, 1), []string{"rub-buy"})
	if e != nil || len(result.LotHistory) != 3 {
		t.Fatal(result.LotHistory, e)
	}
	change := result.LotHistory[2]
	if change.Before.Currency != "RUB" || change.Before.Quantity.RatString() != "4" || change.After.Quantity.RatString() != "1" || change.After.CleanCost.RatString() != "50" {
		t.Fatal(change)
	}
	// The trace owns its values, not pointers to mutable replay state.
	change.After.Quantity.SetInt64(99)
	if result.ExactLots["BOND"][0].Quantity.RatString() != "1" {
		t.Fatal("trace aliases current lots")
	}
}
