package investment

import (
	"errors"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

var day = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func testLedger() d.InvestmentLedger {
	return d.InvestmentLedger{ID: "ledger", TenantID: "tenant", StartedAt: day}
}
func op(id, kind, amount string, seq int64) d.InvestmentOperation {
	return d.InvestmentOperation{ID: id, LedgerID: "ledger", TenantID: "tenant", Kind: kind, Currency: "USD", Amount: amount, OccurredAt: day.Add(time.Duration(seq) * time.Hour), Sequence: seq, Status: "posted"}
}
func TestReplayC01C02C03C14(t *testing.T) {
	deposit := op("deposit", "deposit", "1000", 1)
	buy := op("buy", "buy", "200", 2)
	buy.Quantity = "2"
	buy.Fee = "2"
	buy.InstrumentKey = "TEST"
	buy.SettlementAt = day.AddDate(0, 0, 2)
	sell := op("sell", "sell", "120", 73)
	sell.Quantity = "1"
	sell.Fee = "1"
	sell.InstrumentKey = "TEST"
	dividend := op("dividend", "dividend", "10", 74)
	dividend.Tax = "1"
	dividend.InstrumentKey = "TEST"
	operations := []d.InvestmentOperation{sell, dividend, buy, deposit}
	r, err := Replay(testLedger(), operations, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	cash := r.Snapshot.Cash[0]
	if cash.Settled != "1000" || cash.Payable != "202" || cash.Economic != "798" {
		t.Fatal(cash)
	}
	if r.Snapshot.Positions[0].CleanCost != "202" || r.Snapshot.Positions[0].Quantity != "2" {
		t.Fatal(r.Snapshot.Positions)
	}
	r, err = Replay(testLedger(), operations, day.AddDate(0, 0, 5))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Snapshot.Issues) != 1 || r.Snapshot.Issues[0] != "settlement_assumed_trade_time" {
		t.Fatal("missing settlement quality", r.Snapshot.Issues)
	}
	cash = r.Snapshot.Cash[0]
	p := r.Snapshot.Positions[0]
	if cash.Economic != "926" || cash.Settled != "926" || cash.Payable != "0" || p.CleanCost != "101" || p.RealizedClean != "18" || p.Quantity != "1" {
		t.Fatal(cash, p)
	}
}
func TestReplayRejectsBackdatedShortAndMissingCash(t *testing.T) {
	buy := op("buy", "buy", "100", 2)
	buy.Quantity = "1"
	buy.InstrumentKey = "TEST"
	if _, err := Replay(testLedger(), []d.InvestmentOperation{buy}, day.AddDate(0, 0, 1)); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal(err)
	}
	sell := op("sell", "sell", "100", 1)
	sell.Quantity = "1"
	sell.InstrumentKey = "TEST"
	deposit := op("deposit", "deposit", "1000", 1)
	deposit.OccurredAt = day
	if _, err := Replay(testLedger(), []d.InvestmentOperation{deposit, buy, sell}, day.AddDate(0, 0, 1)); err == nil {
		t.Fatal("accepted sale before purchase")
	}
}
func TestReplaySplitExactAndVoidDependency(t *testing.T) {
	deposit := op("deposit", "deposit", "1000", 1)
	buy := op("buy", "buy", "200", 2)
	buy.Quantity = "2"
	buy.InstrumentKey = "TEST"
	split := op("split", "split", "0", 3)
	split.Currency = ""
	split.InstrumentKey = "TEST"
	split.SplitNew = "2"
	split.SplitOld = "1"
	r, err := Replay(testLedger(), []d.InvestmentOperation{deposit, buy, split}, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if r.Snapshot.Positions[0].Quantity != "4" || r.Snapshot.Positions[0].CleanCost != "200" {
		t.Fatal(r.Snapshot)
	}
	split.SplitOld = "3"
	if _, err := Replay(testLedger(), []d.InvestmentOperation{deposit, buy, split}, day.AddDate(0, 0, 1)); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("fractional shares rounded", err)
	}
	deposit.Status = "voided"
	if _, err := Replay(testLedger(), []d.InvestmentOperation{deposit, buy}, day.AddDate(0, 0, 1)); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("void broke funding", err)
	}
}
func TestReplayAccruedSeparateFromCleanAndActualPrice(t *testing.T) {
	deposit := op("deposit", "deposit", "1000", 1)
	buy := op("buy", "buy", "190", 2)
	buy.Quantity = "2"
	buy.InstrumentKey = "BOND"
	buy.Accrued = "4"
	buy.Fee = "1"
	sell := op("sell", "sell", "100", 3)
	sell.Quantity = "1"
	sell.InstrumentKey = "BOND"
	sell.Accrued = "3"
	sell.Fee = "1"
	r, err := Replay(testLedger(), []d.InvestmentOperation{deposit, buy, sell}, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	p := r.Snapshot.Positions[0]
	if p.CleanCost != "95.5" || p.AcquiredAccrued != "2" || p.RealizedClean != "3.5" || p.RealizedAccrued != "1" {
		t.Fatal(p)
	}
	if r.Snapshot.Cash[0].Economic != "907" {
		t.Fatal(r.Snapshot.Cash)
	}
}
func TestValidateNoSilentlyIgnoredFields(t *testing.T) {
	for _, kind := range []string{"deposit", "withdrawal", "dividend", "split", "unknown"} {
		o := op("x", kind, "10", 1)
		o.Quantity = "2"
		if err := ValidateOperation(&o); err == nil {
			t.Fatal(kind, "accepted incompatible fields")
		}
	}
	o := op("x", "deposit", "1.0000000000001", 1)
	if err := ValidateOperation(&o); err == nil {
		t.Fatal("rounded precision")
	}
}

func TestReplayC08AmortizationAndRedemption(t *testing.T) {
	funding := op("funding", "deposit", "900", 1)
	buy := op("buy", "buy", "900", 2)
	buy.InstrumentKey = "BOND"
	buy.Quantity = "1"
	amort := op("amort", "amortization", "200", 3)
	amort.InstrumentKey = "BOND"
	amort.NominalBefore = "1000"
	amort.NominalAfter = "800"
	amort.SettlementAt = day.Add(5 * time.Hour)
	redeem := op("redeem", "redemption", "800", 6)
	redeem.InstrumentKey = "BOND"
	redeem.Quantity = "1"
	redeem.NominalBefore = "800"
	redeem.NominalAfter = "0"
	redeem.SettlementAt = day.Add(8 * time.Hour)
	ops := []d.InvestmentOperation{funding, buy, amort, redeem}
	r, err := Replay(testLedger(), ops, day.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	p := r.Snapshot.Positions[0]
	cash := r.Snapshot.Cash[0]
	if p.CleanCost != "720" || p.RealizedClean != "20" || p.Quantity != "1" || p.OutstandingNominal != "800" || cash.Economic != "200" || cash.Settled != "0" || cash.Receivable != "200" {
		t.Fatal(p, cash)
	}
	r, err = Replay(testLedger(), ops, day.Add(7*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	p = r.Snapshot.Positions[0]
	cash = r.Snapshot.Cash[0]
	if p.CleanCost != "0" || p.RealizedClean != "100" || p.Quantity != "0" || p.OutstandingNominal != "0" || cash.Settled != "200" || cash.Receivable != "800" {
		t.Fatal(p, cash)
	}
	r, err = Replay(testLedger(), ops, day.Add(9*time.Hour))
	if err != nil || r.Snapshot.Cash[0].Settled != "1000" || len(r.SecurityLegs) != 2 {
		t.Fatal(r, err)
	}
	for _, leg := range r.CashLegs {
		if leg.Component == "income" {
			t.Fatal("principal classified as income")
		}
	}
	redeem.NominalBefore = "1000"
	ops[3] = redeem
	if _, err = Replay(testLedger(), ops, day.Add(9*time.Hour)); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("inconsistent nominal accepted", err)
	}
	redeem.NominalBefore = "800"
	redeem.Quantity = "0.5"
	ops[3] = redeem
	if _, err = Replay(testLedger(), ops, day.Add(9*time.Hour)); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("partial redemption accepted", err)
	}
}

func TestCouponRecoveryByLot(t *testing.T) {
	funding := op("cash", "deposit", "1000", 1)
	buy := op("lot-a", "buy", "200", 2)
	buy.Quantity = "2"
	buy.Accrued = "20"
	buy.InstrumentKey = "BOND"
	coupon := op("coupon", "coupon", "30", 3)
	coupon.InstrumentKey = "BOND"
	coupon.Tax = "3"
	coupon.CouponAllocationConfirmed = true
	coupon.AccruedRecoveries = []d.InvestmentAccruedRecovery{{OriginID: "lot-a", Amount: "20"}}
	sell := op("sell", "sell", "110", 4)
	sell.Quantity = "1"
	sell.Accrued = "5"
	sell.InstrumentKey = "BOND"
	ops := []d.InvestmentOperation{funding, buy, coupon, sell}
	r, err := Replay(testLedger(), ops, day.Add(5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	p := r.Snapshot.Positions[0]
	seenIncome, seenTax := false, false
	for _, event := range r.ProfitEvents {
		if event.OperationID == "coupon" && event.Kind == "earned_income" {
			seenIncome = event.Amount != nil && event.Amount.RatString() == "10"
		}
		if event.OperationID == "coupon" && event.Kind == "taxes" {
			seenTax = event.Amount != nil && event.Amount.RatString() == "-3"
		}
	}
	if !seenIncome || !seenTax {
		t.Fatal("coupon cash or tax counted as earned income", r.ProfitEvents)
	}
	if p.CouponGross != "30" || p.CouponTax != "3" || p.CouponRecovered != "20" || p.CouponEarned != "10" || p.AcquiredAccrued != "0" || p.RealizedAccrued != "5" || p.CleanCost != "100" || r.Snapshot.Cash[0].Economic != "922" || len(p.Lots) != 1 || p.Lots[0].OriginID != "lot-a" {
		t.Fatal(p, r.Snapshot.Cash)
	}
	r, err = Replay(testLedger(), ops, day.Add(150*time.Minute))
	if err != nil || r.Snapshot.Positions[0].AcquiredAccrued != "20" || r.Snapshot.Positions[0].CouponGross != "0" {
		t.Fatal("historical cutoff", r, err)
	}
	// Recovery cannot consume sold or foreign lots, and an excessive allocation
	// cannot be hidden by enough gross coupon cash.
	for _, allocation := range []d.InvestmentAccruedRecovery{{OriginID: "lot-a", Amount: "21"}, {OriginID: "foreign-lot", Amount: "1"}} {
		bad := coupon
		bad.AccruedRecoveries = []d.InvestmentAccruedRecovery{allocation}
		ops[2] = bad
		if _, err = Replay(testLedger(), ops, day.Add(5*time.Hour)); !errors.Is(err, d.ErrInvestmentPrecondition) {
			t.Fatal(allocation, err)
		}
	}
	// Missing confirmation does not invent either recovered or earned interest.
	unknown := coupon
	unknown.CouponAllocationConfirmed = false
	unknown.AccruedRecoveries = nil
	ops[2] = unknown
	r, err = Replay(testLedger(), ops, day.Add(5*time.Hour))
	if err != nil || r.Snapshot.Positions[0].CouponEarned != "" || r.Snapshot.Positions[0].AcquiredAccrued != "10" {
		t.Fatal(r, err)
	}
	found := false
	for _, issue := range r.Snapshot.Issues {
		if issue == "coupon_allocation_required" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing quality issue")
	}
	unknownEvent := false
	for _, event := range r.ProfitEvents {
		if event.OperationID == "coupon" && event.Kind == "earned_income" {
			unknownEvent = event.Amount == nil && event.Reason == "coupon_allocation_required"
		}
	}
	if !unknownEvent {
		t.Fatal("unallocated coupon invented earned income", r.ProfitEvents)
	}
}

func TestFXSettlementAndDependentWithdrawal(t *testing.T) {
	fund := op("fund", "deposit", "200", 1)
	fx := op("fx", "fx", "100", 2)
	fx.ToCurrency = "RUB"
	fx.ToAmount = "9000"
	fx.Fee = "1"
	fx.SettlementAt = day.Add(4 * time.Hour)
	withdraw := op("withdraw", "withdrawal", "9000", 5)
	withdraw.Currency = "RUB"
	ops := []d.InvestmentOperation{fund, fx, withdraw}
	r, err := Replay(testLedger(), ops, day.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	balances := map[string]d.InvestmentCash{}
	for _, c := range r.Snapshot.Cash {
		balances[c.Currency] = c
	}
	if balances["USD"].Settled != "200" || balances["USD"].Payable != "101" || balances["USD"].Economic != "99" || balances["RUB"].Receivable != "9000" || balances["RUB"].Settled != "0" {
		t.Fatal(balances)
	}
	r, err = Replay(testLedger(), ops, day.Add(6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Snapshot.Cash {
		balances[c.Currency] = c
	}
	if balances["USD"].Settled != "99" || balances["RUB"].Settled != "0" || balances["RUB"].Receivable != "0" {
		t.Fatal(balances)
	}
	for _, leg := range r.CashLegs {
		if leg.OperationID == "fx" && (leg.Component == "deposit" || leg.Component == "withdrawal") {
			t.Fatal("FX is not external funding", leg)
		}
	}
	fx.Status = "voided"
	ops[1] = fx
	if _, err = Replay(testLedger(), ops, day.Add(6*time.Hour)); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("void broke funding", err)
	}
	fx.Status = "posted"
	fx.ToCurrency = "USD"
	if err = ValidateOperation(&fx); !errors.Is(err, d.ErrInvestmentInvalid) {
		t.Fatal("same currency accepted", err)
	}
	fx.ToCurrency = "RUB"
	fx.ToAmount = "0"
	if err = ValidateOperation(&fx); !errors.Is(err, d.ErrInvestmentInvalid) {
		t.Fatal("zero target accepted", err)
	}
}

func TestOpeningUnknownCostsAndCorrection(t *testing.T) {
	cash := op("opening-cash", "opening_cash", "100", 1)
	cash.OccurredAt = day
	cash.Note = "Opening statement"
	opening := op("opening-position", "opening_position", "0", 2)
	opening.OccurredAt = day
	opening.InstrumentKey = "STOCK"
	opening.Quantity = "2"
	opening.Note = "Quantity confirmed; acquisition records missing"
	buy := op("buy", "buy", "100", 3)
	buy.InstrumentKey = "STOCK"
	buy.Quantity = "1"
	sell := op("sell", "sell", "330", 4)
	sell.InstrumentKey = "STOCK"
	sell.Quantity = "3"
	sell.Accrued = "5"
	ops := []d.InvestmentOperation{sell, buy, opening, cash}
	r, err := Replay(testLedger(), ops, day.Add(210*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	p := r.Snapshot.Positions[0]
	if p.Quantity != "3" || p.CleanCost != "" || p.AcquiredAccrued != "" || len(p.Lots) != 2 || p.Lots[0].CleanCost != "" || p.Lots[1].CleanCost != "100" {
		t.Fatal(p)
	}
	for _, leg := range r.CashLegs {
		if leg.OperationID == opening.ID {
			t.Fatal("opening position invented cash", leg)
		}
	}
	r, err = Replay(testLedger(), ops, day.Add(5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	p = r.Snapshot.Positions[0]
	if p.Quantity != "0" || p.RealizedClean != "" || p.RealizedAccrued != "" || r.Snapshot.Cash[0].Economic != "335" {
		t.Fatal(p, r.Snapshot.Cash)
	}
	unknownRealized := 0
	for _, event := range r.ProfitEvents {
		if event.OperationID == "sell" && (event.Kind == "realized_clean" || event.Kind == "realized_accrued") {
			if event.Amount != nil {
				t.Fatal("unknown FIFO invented a gain", event)
			}
			unknownRealized++
		}
	}
	if unknownRealized != 2 {
		t.Fatal(r.ProfitEvents)
	}
	opening.OpeningCostKnown = true
	opening.Amount = "180"
	opening.OpeningAccruedKnown = true
	opening.Accrued = "10"
	opening.AcquiredAt = day.AddDate(-1, 0, 0)
	ops[2] = opening
	r, err = Replay(testLedger(), ops, day.Add(5*time.Hour))
	if err != nil || r.Snapshot.Positions[0].RealizedClean != "50" || r.Snapshot.Positions[0].RealizedAccrued != "-5" {
		t.Fatal(r, err)
	}
	for _, issue := range r.Snapshot.Issues {
		if issue == "unknown_acquisition_cost" || issue == "unknown_acquired_interest" || issue == "historical_acquisition_date_unknown" {
			t.Fatal("correction did not restore known values", issue)
		}
	}
	// Opening is a starting state, not a backdated deposit or purchase.
	opening.OccurredAt = day.Add(time.Hour)
	ops[2] = opening
	if _, err = Replay(testLedger(), ops, day.Add(5*time.Hour)); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("opening at later date", err)
	}
	opening.OccurredAt = day
	ops[2] = opening
	secondCash := cash
	secondCash.ID = "duplicate"
	secondCash.Sequence = 10
	ops = append(ops, secondCash)
	if _, err = Replay(testLedger(), ops, day.Add(5*time.Hour)); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("duplicate opening currency", err)
	}
}

func TestReplayExactRemainingCostForProfit(t *testing.T) {
	funding := op("fund", "deposit", "100", 1)
	buy := op("buy", "buy", "1", 2)
	buy.InstrumentKey = "S"
	buy.Quantity = "3"
	sell := op("sell", "sell", "1", 3)
	sell.InstrumentKey = "S"
	sell.Quantity = "1"
	r, e := Replay(testLedger(), []d.InvestmentOperation{funding, buy, sell}, day.Add(4*time.Hour))
	if e != nil || r.ExactPositions["S"].CleanCost.RatString() != "2/3" || r.ExactPositions["S"].Quantity.RatString() != "2" {
		t.Fatal(r, e)
	}
	if r.ExactLots["S"][0].CleanCost.RatString() != "2/3" || !r.ExactLots["S"][0].AcquiredAt.Equal(buy.OccurredAt) {
		t.Fatal(r.ExactLots)
	}
	for _, event := range r.ProfitEvents {
		if event.Kind == "realized_clean" && event.Amount.RatString() != "2/3" {
			t.Fatal(event)
		}
	}
	before, e := Replay(testLedger(), []d.InvestmentOperation{funding, buy, sell}, day.Add(150*time.Minute))
	if e != nil || before.ExactPositions["S"].CleanCost.RatString() != "1" {
		t.Fatal("future sale changed captured exact cost", before, e)
	}
}

func TestReplayRemainingLotsPreserveDatesAndFractionalSplit(t *testing.T) {
	funding := op("fund", "deposit", "100", 1)
	operations := []d.InvestmentOperation{funding}
	for i := int64(2); i < 5; i++ {
		buy := op(string(rune('a'+i)), "buy", "1", i)
		buy.InstrumentKey = "S"
		buy.Quantity = "1"
		operations = append(operations, buy)
	}
	split := op("split", "split", "0", 5)
	split.Currency = ""
	split.InstrumentKey = "S"
	split.SplitNew = "1"
	split.SplitOld = "3"
	operations = append(operations, split)
	got, e := Replay(testLedger(), operations, day.Add(6*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if len(got.ExactLots["S"]) != 3 {
		t.Fatal(got.ExactLots)
	}
	for i, lot := range got.ExactLots["S"] {
		if lot.Quantity.RatString() != "1/3" || lot.CleanCost.RatString() != "1" || !lot.AcquiredAt.Equal(operations[i+1].OccurredAt) || lot.AcquiredAccrued.Sign() != 0 {
			t.Fatal(lot)
		}
	}
	// Later replay mutations cannot alter a captured cutoff.
	before, e := Replay(testLedger(), operations, day.Add(4*time.Hour))
	if e != nil || before.ExactLots["S"][0].Quantity.RatString() != "1" {
		t.Fatal(before, e)
	}
}
