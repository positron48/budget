package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

func TestTransferCivilTimingUsesOneRuleForBothSides(t *testing.T) {
	const from = "11111111-1111-4111-8111-111111111111"
	const to = "22222222-2222-4222-8222-222222222222"
	now := time.Date(2024, 3, 31, 12, 0, 0, 0, time.UTC)
	for _, security := range []bool{false, true} {
		m := TransferMutation{Action: "create", RequestKey: "x", ExpectedFromVersion: 1, ExpectedToVersion: 1, Transfer: d.InvestmentTransfer{FromLedgerID: from, ToLedgerID: to, Currency: "USD", Amount: "1", Timing: &d.InvestmentOperationTiming{Timezone: "Europe/Berlin", OccurredDate: "2024-03-31"}}}
		want := "2024-03-31T21:59:59.999999Z"
		if security {
			m.Transfer.InstrumentKey = "S"
			m.Transfer.Quantity = "1"
			m.Transfer.Amount = "0"
			want = "2024-03-30T23:00:00Z"
		}
		got, e := NormalizeTransferMutation("t", "u", m, now)
		if e != nil || got.Transfer.OccurredAt.Format(time.RFC3339Nano) != want {
			t.Fatal(got, e)
		}
		m.Transfer.OccurredAt = now
		if _, e = NormalizeTransferMutation("t", "u", m, now); e == nil {
			t.Fatal("contradicting precise timestamp accepted")
		}
		m.Transfer.OccurredAt = time.Time{}
		m.Transfer.Timing = &d.InvestmentOperationTiming{Timezone: "Pacific/Apia", OccurredDate: "2011-12-30"}
		if _, e = NormalizeTransferMutation("t", "u", m, now); e == nil {
			t.Fatal("skipped civil date accepted")
		}
	}
}
