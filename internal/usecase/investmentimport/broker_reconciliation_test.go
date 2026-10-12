package investmentimport

import (
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

func TestBrokerJournalMatchesAreConservativeAndExact(t *testing.T) {
	candidate := d.InvestmentOperation{LedgerID: "a", Kind: "buy", InstrumentKey: "uid", Currency: "RUB", Quantity: "9007199254740993", Amount: "12.34", Fee: "0.3", Accrued: "0", Tax: "0", OccurredAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	operation := candidate
	operation.Status = "posted"
	operation.ID = "existing"
	operation.Quantity = "9007199254740993.0"
	operation.Source = &d.InvestmentSource{ImportID: "snowball"}
	matches := PossibleBrokerTradeMatches(candidate, []d.InvestmentOperation{operation})
	if len(matches) != 1 || !matches[0].SameLedger || !matches[0].SameInstrument || !matches[0].ExactTime || !matches[0].SnowballSource {
		t.Fatal(matches)
	}
	operation.LedgerID = "other"
	operation.InstrumentKey = "old-ticker"
	operation.OccurredAt = operation.OccurredAt.Add(-12 * time.Hour)
	matches = PossibleBrokerTradeMatches(candidate, []d.InvestmentOperation{operation, operation})
	if len(matches) != 2 || matches[0].SameLedger || matches[0].SameInstrument || matches[0].ExactTime {
		t.Fatal(matches)
	}
	for _, change := range []func(*d.InvestmentOperation){
		func(o *d.InvestmentOperation) { o.Quantity = "9007199254740992" },
		func(o *d.InvestmentOperation) { o.Fee = "0.300000000001" },
		func(o *d.InvestmentOperation) { o.Status = "voided" },
		func(o *d.InvestmentOperation) {
			o.OccurredAt = candidate.OccurredAt.Add(-24*time.Hour - time.Nanosecond)
		},
		func(o *d.InvestmentOperation) { o.Currency = "USD" },
		func(o *d.InvestmentOperation) { o.Kind = "sell" },
	} {
		altered := operation
		change(&altered)
		if got := PossibleBrokerTradeMatches(candidate, []d.InvestmentOperation{altered}); len(got) != 0 {
			t.Fatal(got)
		}
	}
}
