package investment

import (
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"math/big"
	"testing"
	"time"
)

func TestCashInterestIsReturnNotContribution(t *testing.T) {
	deposit := op("deposit", "deposit", "100", 1)
	interest := op("interest", "cash_interest", "2", 25)
	result, err := Replay(testLedger(), []d.InvestmentOperation{deposit, interest}, day.Add(48*time.Hour))
	if err != nil || len(result.Snapshot.Cash) != 1 || result.Snapshot.Cash[0].Economic != "102" {
		t.Fatal(result, err)
	}
	flows, err := calc.ExternalFlows([]string{"ledger"}, []d.InvestmentOperation{deposit, interest})
	if err != nil || len(flows) != 1 || flows[0].OperationID != "deposit" {
		t.Fatal(flows, err)
	}
	twr, err := calc.DailyTWR([]calc.PerformanceDay{{Date: "2024-01-01", NAV: big.NewRat(100, 1), ExternalFlow: big.NewRat(100, 1), Complete: true}, {Date: "2024-01-02", NAV: big.NewRat(102, 1), ExternalFlow: big.NewRat(0, 1), Complete: true}})
	if err != nil || twr.Return == nil || twr.Return.Cmp(big.NewRat(2, 100)) != 0 {
		t.Fatal(twr, err)
	}
	for _, change := range []func(*d.InvestmentOperation){func(o *d.InvestmentOperation) { o.Amount = "0" }, func(o *d.InvestmentOperation) { o.InstrumentKey = "TEST" }, func(o *d.InvestmentOperation) { o.Tax = "1" }, func(o *d.InvestmentOperation) { o.SettlementAt = o.OccurredAt.Add(time.Hour) }} {
		changed := interest
		change(&changed)
		if ValidateOperation(&changed) == nil {
			t.Fatal("invalid cash interest accepted", changed)
		}
	}
	interest.Status = "voided"
	result, err = Replay(testLedger(), []d.InvestmentOperation{deposit, interest}, day.Add(48*time.Hour))
	if err != nil || result.Snapshot.Cash[0].Economic != "100" {
		t.Fatal(result, err)
	}
}

func TestTaxReviewFlagPreservesCashAndAsOf(t *testing.T) {
	income := op("income", "cash_interest", "10", 2)
	income.TaxReviewRequired = true
	for _, test := range []struct {
		at   time.Time
		want bool
	}{{day.Add(time.Hour), false}, {day.Add(3 * time.Hour), true}} {
		r, err := Replay(testLedger(), []d.InvestmentOperation{income}, test.at)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, issue := range r.Snapshot.Issues {
			if issue == "tax_review_required" {
				found = true
			}
		}
		if found != test.want {
			t.Fatal(r)
		}
		if test.want && r.Snapshot.Cash[0].Economic != "10" {
			t.Fatal(r)
		}
	}
	income.Kind = "deposit"
	if ValidateOperation(&income) == nil {
		t.Fatal("tax review on non-income")
	}
}
