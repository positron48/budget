package investmentvaluation

import (
	"math/big"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

func TestHistoricalRebaseRetainsReviewedZeroEstimate(t *testing.T) {
	day := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	splits := []d.InvestmentOperation{{Kind: "split", Status: "posted", InstrumentKey: "S", SplitOld: "1", SplitNew: "3", OccurredAt: day.Add(24 * time.Hour)}}
	price, err := RebaseHistoricalPrice(new(big.Rat), day, day.Add(48*time.Hour), "S", splits)
	if err != nil || price.Sign() != 0 {
		t.Fatal("reviewed zero estimate broke history", price, err)
	}
	if _, err := RebaseHistoricalPrice(big.NewRat(-1, 1), day, day.Add(48*time.Hour), "S", splits); err == nil {
		t.Fatal("negative quote accepted")
	}
}

func TestRetrospectiveSplitDoesNotMultiplyOldCloseByNewUnits(t *testing.T) {
	friday := time.Date(2024, 6, 7, 0, 0, 0, 0, time.UTC)
	split := d.InvestmentOperation{Kind: "split", Status: "posted", InstrumentKey: "S", SplitOld: "1", SplitNew: "10", OccurredAt: friday.AddDate(0, 0, 2)}
	price := big.NewRat(1200, 1)
	rebased, err := RebaseHistoricalPrice(price, friday.AddDate(0, 0, 1), friday.AddDate(0, 0, 3).Add(21*time.Hour), "S", []d.InvestmentOperation{split})
	if err != nil || rebased.RatString() != "120" || price.RatString() != "1200" {
		t.Fatal(rebased, err)
	}
	if new(big.Rat).Mul(rebased, big.NewRat(10, 1)).RatString() != "1200" {
		t.Fatal("split changed recorded total value")
	}
	// The Monday post-split close is already in the new units. Never divide twice.
	rebased, err = RebaseHistoricalPrice(big.NewRat(121, 1), friday.AddDate(0, 0, 4), friday.AddDate(0, 0, 4).Add(time.Hour), "S", []d.InvestmentOperation{split})
	if err != nil || rebased.RatString() != "121" {
		t.Fatal("raw post-split candle adjusted twice", rebased, err)
	}
	// Different accounts/instruments and future/voided operations cannot affect it.
	split.Status = "voided"
	rebased, err = RebaseHistoricalPrice(price, friday.AddDate(0, 0, 1), friday.AddDate(0, 0, 3), "S", []d.InvestmentOperation{split})
	if err != nil || rebased.RatString() != "1200" {
		t.Fatal(rebased, err)
	}
}

func TestHistoricalReverseAndMultipleSplitRatiosStayExact(t *testing.T) {
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	splits := []d.InvestmentOperation{{Kind: "split", Status: "posted", InstrumentKey: "S", SplitOld: "2", SplitNew: "1", OccurredAt: day.AddDate(0, 0, 1)}, {Kind: "split", Status: "posted", InstrumentKey: "S", SplitOld: "1", SplitNew: "3", OccurredAt: day.AddDate(0, 0, 2)}}
	p, err := RebaseHistoricalPrice(big.NewRat(17, 3), day.AddDate(0, 0, 1), day.AddDate(0, 0, 3), "S", splits)
	if err != nil || p.RatString() != "34/9" {
		t.Fatal(p, err)
	}
	splits[1].SplitNew = "0"
	if _, err = RebaseHistoricalPrice(big.NewRat(1, 1), day.AddDate(0, 0, 1), day.AddDate(0, 0, 3), "S", splits); err == nil {
		t.Fatal("invalid recorded ratio accepted")
	}
}

func TestWeekdaySplitAtMoscowMidnightRebasesPreviousUTCSession(t *testing.T) {
	day := time.Date(2022, 8, 24, 0, 0, 0, 0, time.UTC)
	op := d.InvestmentOperation{Kind: "split", Status: "posted", InstrumentKey: "S", SplitOld: "1", SplitNew: "3", OccurredAt: day.Add(21 * time.Hour)}
	p, err := RebaseHistoricalPrice(big.NewRat(900, 1), day.Add(24*time.Hour), day.Add(45*time.Hour-time.Microsecond), "S", []d.InvestmentOperation{op})
	if err != nil || p.RatString() != "300" {
		t.Fatal("Moscow date treated as preceding UTC day", p, err)
	}
	p, err = RebaseHistoricalPrice(big.NewRat(310, 1), day.Add(48*time.Hour), day.Add(69*time.Hour-time.Microsecond), "S", []d.InvestmentOperation{op})
	if err != nil || p.RatString() != "310" {
		t.Fatal("post-split close divided twice", p, err)
	}
}
