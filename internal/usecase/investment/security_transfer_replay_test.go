package investment

import (
	"errors"
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"reflect"
	"slices"
	"testing"
	"time"
)

func portfolioFixture() ([]d.InvestmentLedger, []d.InvestmentOperation) {
	ledgers := []d.InvestmentLedger{{ID: "a", TenantID: "tenant", StartedAt: day}, {ID: "b", TenantID: "tenant", StartedAt: day}, {ID: "c", TenantID: "tenant", StartedAt: day}}
	cash := op("cash", "deposit", "100", 1)
	cash.LedgerID = "a"
	buy := op("buy", "buy", "1", 2)
	buy.LedgerID = "a"
	buy.Quantity = "3"
	buy.InstrumentKey = "S"
	buy.Accrued = "6"
	out := op("out", "security_transfer_out", "0", 3)
	out.LedgerID = "a"
	out.TransferID = "move"
	out.CounterpartyLedgerID = "b"
	out.InstrumentKey = "S"
	out.Quantity = "2"
	in := out
	in.ID = "in"
	in.LedgerID = "b"
	in.CounterpartyLedgerID = "a"
	in.Kind = "security_transfer_in"
	in.Sequence = 1
	return ledgers, []d.InvestmentOperation{cash, buy, out, in}
}
func TestSecurityTransferPreservesExactLotsAndIndependentSlices(t *testing.T) {
	ledgers, ops := portfolioFixture()
	before := slices.Clone(ops)
	got, e := ReplayPortfolio(ledgers, ops, day.Add(24*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(ops, before) {
		t.Fatal("input mutated")
	}
	a, b := got.Ledgers["a"], got.Ledgers["b"]
	original, incoming := a.ExactLots["S"][0], b.ExactLots["S"][0]
	if original.Quantity.RatString() != "1" || original.CleanCost.RatString() != "1/3" || incoming.Quantity.RatString() != "2" || incoming.CleanCost.RatString() != "2/3" || incoming.AcquiredAccrued.RatString() != "4" || incoming.OriginID != "buy" || incoming.OriginLedgerID != "a" || incoming.OriginSequence != 2 || !incoming.AcquiredAt.Equal(ops[1].OccurredAt) || incoming.LotID == original.LotID {
		t.Fatal(original, incoming)
	}
	if len(b.Snapshot.Cash) != 0 || a.Snapshot.Cash[0].Economic != "93" || len(b.ProfitEvents) != 0 {
		t.Fatal(a, b)
	}
	// Recover interest only in the destination, then return one unit. Keeping the
	// returned part separate avoids averaging its zero interest with the 2 units
	// of acquired interest still held by the original source lot.
	coupon := op("coupon", "coupon", "4", 2)
	coupon.LedgerID = "b"
	coupon.InstrumentKey = "S"
	coupon.OccurredAt = day.Add(4 * time.Hour)
	coupon.CouponAllocationConfirmed = true
	coupon.AccruedRecoveries = []d.InvestmentAccruedRecovery{{OriginID: incoming.LotID, AllRemaining: true}}
	out := ops[2]
	out.ID = "back-out"
	out.TransferID = "back"
	out.LedgerID = "b"
	out.CounterpartyLedgerID = "a"
	out.Sequence = 3
	out.Quantity = "1"
	out.OccurredAt = day.Add(5 * time.Hour)
	in := out
	in.ID = "back-in"
	in.Kind = "security_transfer_in"
	in.LedgerID = "a"
	in.CounterpartyLedgerID = "b"
	in.Sequence = 4
	ops = append(ops, coupon, out, in)
	roundtrip, e := ReplayPortfolio(ledgers, ops, day.Add(24*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	lots := roundtrip.Ledgers["a"].ExactLots["S"]
	if len(lots) != 2 || lots[0].AcquiredAccrued.RatString() != "2" || lots[1].AcquiredAccrued.Sign() != 0 || lots[0].LotID == lots[1].LotID || lots[1].OriginID != "buy" || lots[1].CleanCost.RatString() != "1/3" {
		t.Fatal(lots)
	}
	sell := op("sell", "sell", "1", 5)
	sell.LedgerID = "a"
	sell.InstrumentKey = "S"
	sell.Quantity = "1"
	sell.Accrued = "2"
	sell.OccurredAt = day.Add(6 * time.Hour)
	ops = append(ops, sell)
	sold, e := ReplayPortfolio(ledgers, ops, day.Add(24*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if sold.Ledgers["a"].ExactLots["S"][0].AcquiredAccrued.Sign() != 0 || sold.Ledgers["a"].Snapshot.Positions[0].RealizedAccrued != "0" {
		t.Fatal(sold.Ledgers["a"])
	}
	// A changed acquisition recomputes all downstream transfer receipts exactly.
	ops[1].Amount = "2"
	corrected, e := ReplayPortfolio(ledgers, ops, day.Add(24*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if corrected.Ledgers["a"].ExactLots["S"][0].CleanCost.RatString() != "2/3" || corrected.Ledgers["b"].ExactLots["S"][0].CleanCost.RatString() != "2/3" {
		t.Fatal(corrected)
	}
	reversedLedgers, reversedOps := slices.Clone(ledgers), slices.Clone(ops)
	slices.Reverse(reversedLedgers)
	slices.Reverse(reversedOps)
	again, e := ReplayPortfolio(reversedLedgers, reversedOps, day.Add(24*time.Hour))
	if e != nil || !reflect.DeepEqual(corrected, again) {
		t.Fatal("order-dependent replay", e)
	}
	got.Transfers["move"].Lots[0].CleanCost.SetInt64(100)
	if incoming.CleanCost.Cmp(big.NewRat(2, 3)) != 0 {
		t.Fatal("receipt aliases receiver")
	}
}
func TestSecurityTransferMissingPairCyclesAndFutureValidation(t *testing.T) {
	ledgers, ops := portfolioFixture()
	if _, e := Replay(ledgers[0], ops[:3], day.Add(24*time.Hour)); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(e)
	}
	if _, e := ReplayPortfolio(ledgers, ops[:3], day.Add(24*time.Hour)); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(e)
	}
	for _, change := range []func([]d.InvestmentOperation){func(v []d.InvestmentOperation) { v[3].Quantity = "1" }, func(v []d.InvestmentOperation) { v[3].Currency = "EUR" }, func(v []d.InvestmentOperation) { v[3].Status = "voided" }, func(v []d.InvestmentOperation) { v[3].TenantID = "other" }, func(v []d.InvestmentOperation) { v[2].Quantity = "4"; v[3].Quantity = "4" }, func(v []d.InvestmentOperation) { v[3].OccurredAt = v[3].OccurredAt.Add(time.Microsecond) }} {
		copy := slices.Clone(ops)
		change(copy)
		if _, e := ReplayPortfolio(ledgers, copy, day.Add(24*time.Hour)); e == nil {
			t.Fatal("invalid transfer accepted", copy)
		}
	}
	earlier, e := ReplayPortfolio(ledgers, ops, ops[1].OccurredAt)
	if e != nil || len(earlier.Ledgers["b"].ExactLots) != 0 || earlier.Ledgers["a"].ExactLots["S"][0].Quantity.RatString() != "3" {
		t.Fatal(earlier, e)
	}
	bad := slices.Clone(ops)
	bad[2].Quantity = "4"
	bad[3].Quantity = "4"
	if _, e = ReplayPortfolio(ledgers, bad, ops[1].OccurredAt); e == nil {
		t.Fatal("future short transfer ignored")
	}
	// At one timestamp, each account waits for the other incoming transfer.
	xout := ops[2]
	xout.ID = "xout"
	xout.TransferID = "x"
	xout.Sequence = 4
	xin := ops[3]
	xin.ID = "xin"
	xin.TransferID = "x"
	xin.Sequence = 1
	yout := ops[3]
	yout.ID = "yout"
	yout.Kind = "security_transfer_out"
	yout.TransferID = "y"
	yout.Sequence = 2
	yin := ops[2]
	yin.ID = "yin"
	yin.Kind = "security_transfer_in"
	yin.TransferID = "y"
	yin.Sequence = 3
	if _, e = ReplayPortfolio(ledgers, []d.InvestmentOperation{ops[0], ops[1], xout, xin, yout, yin}, day.Add(24*time.Hour)); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("cycle accepted", e)
	}
}

func TestSecurityTransferUnknownCostAtomicTimestampAndDependentVoid(t *testing.T) {
	ledgers, ops := portfolioFixture()
	unknown := slices.Clone(ops)
	unknown[1].Kind = "opening_position"
	unknown[1].Amount = "0"
	unknown[1].Accrued = "0"
	unknown[1].OccurredAt = day
	unknown[1].Note = "Quantity only"
	result, e := ReplayPortfolio(ledgers, unknown, day.Add(24*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	lot := result.Ledgers["b"].ExactLots["S"][0]
	if lot.CleanCost != nil || lot.AcquiredAccrued != nil || !lot.AcquiredAt.IsZero() || !slices.Contains(result.Ledgers["b"].Snapshot.Issues, "historical_acquisition_date_unknown") || result.Ledgers["b"].Snapshot.Positions[0].Lots[0].AcquisitionOperationID != "buy" {
		t.Fatal(result)
	}
	atomic := slices.Clone(ops)
	at := day.Add(3 * time.Hour)
	atomic[0].OccurredAt = at
	atomic[0].Sequence = 3
	atomic[1].OccurredAt = at
	atomic[1].Sequence = 1
	atomic[2].Sequence = 2
	atomic[2].Quantity = "2.0"
	if _, e = ReplayPortfolio(ledgers, atomic, day.Add(24*time.Hour)); e != nil {
		t.Fatal("same-time source prefix wrongly required settled cash", e)
	}
	sold := slices.Clone(ops)
	sale := op("destination-sale", "sell", "1", 2)
	sale.LedgerID = "b"
	sale.InstrumentKey = "S"
	sale.Quantity = "1"
	sale.OccurredAt = day.Add(4 * time.Hour)
	sold = append(sold, sale)
	if _, e = ReplayPortfolio(ledgers, sold, day.Add(24*time.Hour)); e != nil {
		t.Fatal(e)
	}
	sold[2].Status = "voided"
	sold[3].Status = "voided"
	if _, e = ReplayPortfolio(ledgers, sold, day.Add(24*time.Hour)); e == nil {
		t.Fatal("void detached a future dependent sale")
	}
	// A fully voided pair with no dependent facts removes both sides together.
	ops[2].Status = "voided"
	ops[3].Status = "voided"
	voided, e := ReplayPortfolio(ledgers, ops, day.Add(24*time.Hour))
	if e != nil || len(voided.Transfers) != 0 || len(voided.Ledgers["b"].ExactLots) != 0 || voided.Ledgers["a"].ExactLots["S"][0].Quantity.RatString() != "3" {
		t.Fatal(voided, e)
	}
}
func TestSecurityTransferConservesEveryExactCostComponent(t *testing.T) {
	for i := 1; i <= 30; i++ {
		ledgers, ops := portfolioFixture()
		quantity := new(big.Rat).SetFrac64(int64(i), 10).FloatString(1)
		ops[2].Quantity = quantity
		ops[3].Quantity = quantity
		v, e := ReplayPortfolio(ledgers, ops, day.Add(24*time.Hour))
		if e != nil {
			t.Fatal(i, e)
		}
		q, c, a := new(big.Rat), new(big.Rat), new(big.Rat)
		for _, ledger := range v.Ledgers {
			for _, lot := range ledger.ExactLots["S"] {
				q.Add(q, lot.Quantity)
				c.Add(c, lot.CleanCost)
				a.Add(a, lot.AcquiredAccrued)
			}
		}
		if q.Cmp(big.NewRat(3, 1)) != 0 || c.Cmp(big.NewRat(1, 1)) != 0 || a.Cmp(big.NewRat(6, 1)) != 0 {
			t.Fatal(i, q, c, a)
		}
	}
}

func TestSecurityTransferAcrossDistributionAndThirdAccount(t *testing.T) {
	ledgers, ops := portfolioFixture()
	ops[1].Accrued = "0"
	action := op("action", "distribution", "0", 2)
	action.LedgerID = "b"
	action.InstrumentKey = "S"
	action.Note = "Reviewed terms"
	action.OccurredAt = day.Add(4 * time.Hour)
	action.Distribution = &d.InvestmentDistribution{EventKey: "event", TargetInstrumentKey: "NEW", NewUnits: "2", OldUnits: "1", CostNumerator: "1", CostDenominator: "2"}
	out := ops[2]
	out.ID = "third-out"
	out.LedgerID = "b"
	out.CounterpartyLedgerID = "c"
	out.Sequence = 3
	out.InstrumentKey = "NEW"
	out.Quantity = "1"
	out.TransferID = "third"
	out.OccurredAt = day.Add(5 * time.Hour)
	in := out
	in.ID = "third-in"
	in.Kind = "security_transfer_in"
	in.LedgerID = "c"
	in.CounterpartyLedgerID = "b"
	in.Sequence = 1
	v, e := ReplayPortfolio(ledgers, append(ops, action, out, in), day.Add(24*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	lot := v.Ledgers["c"].ExactLots["NEW"][0]
	if lot.OriginID != "buy" || lot.OriginLedgerID != "a" || lot.OriginSequence != 2 || !lot.AcquiredAt.Equal(ops[1].OccurredAt) || lot.CleanCost.RatString() != "1/12" {
		t.Fatal(lot)
	}
	snapshot := v.Ledgers["c"].Snapshot.Positions[0].Lots[0]
	if snapshot.OriginID != lot.LotID || snapshot.AcquisitionOperationID != "buy" || snapshot.OriginLedgerID != "a" {
		t.Fatal(snapshot)
	}
	sum := new(big.Rat)
	for _, ledger := range v.Ledgers {
		for _, lots := range ledger.ExactLots {
			for _, lot := range lots {
				sum.Add(sum, lot.CleanCost)
			}
		}
	}
	if sum.Cmp(big.NewRat(1, 1)) != 0 {
		t.Fatal(sum)
	}
}

func TestSecurityTransferLotHistoryFollowsOriginalAcquisition(t *testing.T) {
	ledgers, ops := portfolioFixture()
	v, e := ReplayPortfolioLotHistory(ledgers, ops, day.Add(24*time.Hour), []string{"buy"})
	if e != nil {
		t.Fatal(e)
	}
	source, destination := v.Ledgers["a"].LotHistory, v.Ledgers["b"].LotHistory
	if len(source) != 2 || len(destination) != 1 || source[0].Kind != "buy" || source[1].Kind != "security_transfer_out" || destination[0].Kind != "security_transfer_in" || source[1].After.Quantity.RatString() != "1" || destination[0].Before != nil || destination[0].After.OriginID != "buy" || destination[0].After.LotID == source[1].After.LotID {
		t.Fatal(source, destination)
	}
	prior, e := ReplayPortfolioLotHistory(ledgers, ops, ops[1].OccurredAt, []string{"buy"})
	if e != nil || len(prior.Ledgers["a"].LotHistory) != 1 || len(prior.Ledgers["b"].LotHistory) != 0 {
		t.Fatal(prior, e)
	}
}

func TestSecurityTransferSameTimestampOrderedRoundtrip(t *testing.T) {
	ledgers, ops := portfolioFixture()
	out := ops[3]
	out.ID = "return-out"
	out.Kind = "security_transfer_out"
	out.TransferID = "return"
	out.Sequence = 2
	out.Quantity = "1"
	in := ops[2]
	in.ID = "return-in"
	in.Kind = "security_transfer_in"
	in.TransferID = "return"
	in.Sequence = 4
	in.Quantity = "1"
	v, e := ReplayPortfolio(ledgers, append(ops, out, in), day.Add(24*time.Hour))
	if e != nil {
		t.Fatal("ordered roundtrip mistaken for a cycle", e)
	}
	if len(v.Ledgers["a"].ExactLots["S"]) != 2 || v.Ledgers["b"].ExactLots["S"][0].Quantity.RatString() != "1" {
		t.Fatal(v)
	}
}
