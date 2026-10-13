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
