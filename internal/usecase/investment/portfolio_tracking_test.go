package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"slices"
	"testing"
)

func TestPortfolioPurchaseWithoutFundingAndCouponWithoutTaxReview(t *testing.T) {
	ledger := testLedger()
	ledger.PortfolioTracking = true
	buy := op("buy", "buy", "200", 1)
	buy.InstrumentKey, buy.Quantity, buy.Fee = "BOND", "2", "2"
	sell := op("sell", "sell", "120", 2)
	sell.InstrumentKey, sell.Quantity, sell.Fee = "BOND", "1", "1"
	coupon := op("coupon", "coupon", "15", 3)
	coupon.InstrumentKey, coupon.TaxReviewRequired = "BOND", true
	result, err := Replay(ledger, []d.InvestmentOperation{buy, sell, coupon}, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	position := result.Snapshot.Positions[0]
	if position.Quantity != "1" || position.CleanCost != "101" || position.RealizedClean != "18" || position.CouponEarned != "15" {
		t.Fatal(position)
	}
	if len(result.Snapshot.Cash) != 0 || slices.Contains(result.Snapshot.Issues, "tax_review_required") || slices.Contains(result.Snapshot.Issues, "negative_imported_cash") {
		t.Fatal(result.Snapshot)
	}
}

func TestPortfolioHistoryDoesNotRequireAnAccountOpeningDate(t *testing.T) {
	ledger := testLedger()
	ledger.PortfolioTracking = true
	ledger.StartedAt = day.AddDate(1, 0, 0)
	buy := op("buy", "buy", "100", 1)
	buy.InstrumentKey, buy.Quantity = "STOCK", "1"
	result, err := Replay(ledger, []d.InvestmentOperation{buy}, day.AddDate(0, 0, 1))
	if err != nil || len(result.Snapshot.Positions) != 1 || result.Snapshot.Positions[0].Quantity != "1" {
		t.Fatal(result, err)
	}
}

func TestPortfolioSplitBeforeFirstPurchaseIsNotAnOpeningPosition(t *testing.T) {
	ledger := testLedger()
	ledger.PortfolioTracking = true
	split := op("calendar-split", "split", "0", 1)
	split.InstrumentKey, split.Currency, split.SplitNew, split.SplitOld = "STOCK", "", "4", "1"
	buy := op("later-buy", "buy", "100", 2)
	buy.InstrumentKey, buy.Quantity, buy.Currency = "STOCK", "2", "USD"
	later := op("held-split", "split", "0", 3)
	later.InstrumentKey, later.Currency, later.SplitNew, later.SplitOld = "STOCK", "", "3", "1"
	result, err := Replay(ledger, []d.InvestmentOperation{split, buy, later}, day.AddDate(0, 0, 1))
	if err != nil || len(result.Snapshot.Positions) != 1 || result.Snapshot.Positions[0].Quantity != "6" || result.Snapshot.Positions[0].CleanCost != "100" {
		t.Fatal(result, err)
	}
	ledger.PortfolioTracking = false
	if _, err = Replay(ledger, []d.InvestmentOperation{split, buy, later}, day.AddDate(0, 0, 1)); err == nil {
		t.Fatal("cash-account corporate-event contract changed")
	}
}

func TestPortfolioFullRedemptionNeedsUnitsAndProceedsButNotFaceValue(t *testing.T) {
	ledger := testLedger()
	ledger.PortfolioTracking = true
	buy := op("bond-buy", "buy", "190", 1)
	buy.InstrumentKey, buy.Quantity = "BOND", "2"
	redeem := op("bond-redemption", "redemption", "200", 2)
	redeem.InstrumentKey, redeem.Quantity = "BOND", "2"
	result, err := Replay(ledger, []d.InvestmentOperation{buy, redeem}, day.AddDate(0, 0, 1))
	if err != nil || result.Snapshot.Positions[0].Quantity != "0" || result.Snapshot.Positions[0].RealizedClean != "10" {
		t.Fatal(result, err)
	}
	redeem.Quantity = "1"
	if _, err = Replay(ledger, []d.InvestmentOperation{buy, redeem}, day.AddDate(0, 0, 1)); err == nil {
		t.Fatal("partial quantity masqueraded as full redemption")
	}
}
