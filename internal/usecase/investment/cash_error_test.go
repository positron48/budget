package investment

import (
	"errors"
	d "github.com/positron48/budget/internal/domain"
	"reflect"
	"testing"
	"time"
)

func TestCashErrorReportsAtomicTimestampAndStableCurrency(t *testing.T) {
	a := op("a", "withdrawal", "2", 1)
	a.Currency = "RUB"
	b := op("b", "withdrawal", "3", 2)
	b.Currency = "RUB"
	b.OccurredAt = a.OccurredAt
	c := op("c", "withdrawal", "1", 3)
	c.Currency = "USD"
	c.OccurredAt = a.OccurredAt
	for i := 0; i < 20; i++ {
		_, err := Replay(testLedger(), []d.InvestmentOperation{c, b, a}, day.Add(24*time.Hour))
		var detail *ReplayCashError
		if !errors.Is(err, d.ErrInvestmentPrecondition) || !errors.As(err, &detail) {
			t.Fatal(err)
		}
		if detail.Balance.Currency != "RUB" || detail.Balance.Economic != "-5" || detail.Balance.Settled != "-5" || !reflect.DeepEqual(detail.OperationIDs, []string{"a", "b"}) || !detail.At.Equal(a.OccurredAt) {
			t.Fatal(detail)
		}
		if err.Error() != "investment history is inconsistent: negative cash; confirm opening balance or missing funding" {
			t.Fatal(err)
		}
	}
	// Same-time funding must be applied before declaring a deficit.
	fund := op("fund", "deposit", "5", 4)
	fund.Currency = "RUB"
	fund.OccurredAt = a.OccurredAt
	if _, err := Replay(testLedger(), []d.InvestmentOperation{b, a, fund}, day.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
}
