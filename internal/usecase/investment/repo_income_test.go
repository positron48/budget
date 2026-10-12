package investment

import (
	"errors"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"testing"
	"time"
)

func TestRepoIncomePreservesSecurities(t *testing.T) {
	deposit := op("deposit", "deposit", "100", 1)
	buy := op("buy", "buy", "80", 2)
	buy.InstrumentKey = "TEST"
	buy.Quantity = "2"
	income := op("repo", "repo_income", "10", 3)
	income.InstrumentKey = "TEST"
	income.Tax = "1"
	operations := []d.InvestmentOperation{deposit, buy, income}
	result, err := Replay(testLedger(), operations, day.Add(24*time.Hour))
	if err != nil || result.Snapshot.Cash[0].Economic != "29" || len(result.SecurityLegs) != 1 || result.Snapshot.Positions[0].Quantity != "2" || result.Snapshot.Positions[0].CleanCost != "80" {
		t.Fatal(result, err)
	}
	flows, err := calc.ExternalFlows([]string{"ledger"}, operations)
	if err != nil || len(flows) != 1 || flows[0].OperationID != "deposit" {
		t.Fatal(flows, err)
	}
	for _, change := range []func(*d.InvestmentOperation){func(o *d.InvestmentOperation) { o.InstrumentKey = "" }, func(o *d.InvestmentOperation) { o.Quantity = "1" }, func(o *d.InvestmentOperation) { o.Tax = "11" }} {
		bad := income
		change(&bad)
		if ValidateOperation(&bad) == nil {
			t.Fatal("invalid repo income accepted", bad)
		}
	}
}

func TestReplayOperationErrorKeepsDomainClassification(t *testing.T) {
	sell := op("missing-position", "sell", "10", 1)
	sell.InstrumentKey = "TEST"
	sell.Quantity = "1"
	_, err := Replay(testLedger(), []d.InvestmentOperation{sell}, day.Add(24*time.Hour))
	var detail *ReplayOperationError
	if !errors.Is(err, d.ErrInvestmentPrecondition) || !errors.As(err, &detail) || detail.OperationID != sell.ID || detail.InstrumentKey != "TEST" {
		t.Fatal(err, detail)
	}
	if err.Error() != "investment history is inconsistent: short sale is not supported" {
		t.Fatal("public error changed", err)
	}
}
