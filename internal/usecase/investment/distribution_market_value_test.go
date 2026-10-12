package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"reflect"
	"testing"
	"time"
)

func TestDistributionMarketValueIsSeparateFromCashAndFIFO(t *testing.T) {
	ops := distributionHistory()
	before, err := Replay(testLedger(), ops, day.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	terms := *ops[3].Distribution
	terms.MarketValue = "9007199254740993.123456789012"
	terms.MarketValueEvidence = "Reviewed total value in event currency at event time"
	ops[3].Distribution = &terms
	after, err := Replay(testLedger(), ops, day.Add(24*time.Hour))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("market allocation changed native ledger", err)
	}
	if ops[3].Distribution.MarketValue != terms.MarketValue {
		t.Fatal("mutated caller evidence")
	}
}

func TestDistributionMarketValueRejectsPartialAndInvalidEvidence(t *testing.T) {
	for _, v := range []d.InvestmentDistribution{
		{MarketValue: "1"}, {MarketValueEvidence: "evidence"},
		{MarketValue: "-1", MarketValueEvidence: "evidence"},
		{MarketValue: "NaN", MarketValueEvidence: "evidence"},
		{MarketValue: "1.0000000000001", MarketValueEvidence: "evidence"},
	} {
		op := distributionHistory()[3]
		terms := *op.Distribution
		terms.MarketValue, terms.MarketValueEvidence = v.MarketValue, v.MarketValueEvidence
		op.Distribution = &terms
		if err := ValidateOperation(&op); err == nil {
			t.Fatal("invalid evidence accepted", v)
		}
	}
	for _, value := range []string{"0", "1", "1.123456789012"} {
		op := distributionHistory()[3]
		terms := *op.Distribution
		terms.MarketValue, terms.MarketValueEvidence = value, "Reviewed event value"
		op.Distribution = &terms
		if err := ValidateOperation(&op); err != nil {
			t.Fatal(value, err)
		}
	}
}
