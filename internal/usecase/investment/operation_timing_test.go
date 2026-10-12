package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestDateOnlyCivilBoundariesDSTAndReplay(t *testing.T) {
	for _, tc := range []struct{ zone, date, begin, end string }{
		{"America/Sao_Paulo", "2018-11-04", "2018-11-04T03:00:00Z", "2018-11-05T01:59:59.999999Z"},
		{"Pacific/Apia", "2011-12-29", "2011-12-29T10:00:00Z", "2011-12-30T09:59:59.999999Z"},
		{"Europe/Moscow", "2024-01-01", "2023-12-31T21:00:00Z", "2024-01-01T20:59:59.999999Z"},
		{"America/New_York", "2024-03-10", "2024-03-10T05:00:00Z", "2024-03-11T03:59:59.999999Z"},
		{"America/New_York", "2024-11-03", "2024-11-03T04:00:00Z", "2024-11-04T04:59:59.999999Z"},
	} {
		o := op("buy", "buy", "10", 2)
		o.Quantity = "1"
		o.InstrumentKey = "stock"
		o.OccurredAt = time.Time{}
		o.Timing = &d.InvestmentOperationTiming{Timezone: tc.zone, OccurredDate: tc.date, SettlementDate: tc.date}
		if e := ValidateOperation(&o); e != nil || o.OccurredAt.Format(time.RFC3339Nano) != tc.begin || o.SettlementAt.Format(time.RFC3339Nano) != tc.end || o.SettlementAssumed {
			t.Fatal(tc, o, e)
		}
		if e := ValidateOperation(&o); e != nil {
			t.Fatal("normalization not repeatable", e)
		}
		l := testLedger()
		l.StartedAt = o.OccurredAt.Add(-time.Hour)
		fund := op("fund", "deposit", "100", 1)
		fund.OccurredAt = l.StartedAt
		r, e := Replay(l, []d.InvestmentOperation{o, fund}, o.OccurredAt)
		if e != nil || r.Snapshot.Cash[0].Settled != "100" || r.Snapshot.Cash[0].Payable != "10" {
			t.Fatal(r, e)
		}
		r, e = Replay(l, []d.InvestmentOperation{o, fund}, o.SettlementAt)
		if e != nil || r.Snapshot.Cash[0].Settled != "90" || r.Snapshot.Cash[0].Payable != "0" {
			t.Fatal(r, e)
		}
	}
}
func TestDateOnlyValidationAndAssumedSettlement(t *testing.T) {
	base := op("buy", "buy", "10", 2)
	base.InstrumentKey = "stock"
	base.Quantity = "1"
	base.OccurredAt = time.Time{}
	base.Timing = &d.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: "2024-01-01", SourceDate: "2024-01-01"}
	if e := ValidateOperation(&base); e != nil || !base.SettlementAssumed || !base.SettlementAt.Equal(base.OccurredAt) {
		t.Fatal(base, e)
	}
	for _, terms := range []d.InvestmentOperationTiming{{Timezone: "Local", OccurredDate: "2024-01-01"}, {Timezone: "invalid", OccurredDate: "2024-01-01"}, {Timezone: "UTC", OccurredDate: "2024-02-30"}, {Timezone: "UTC", OccurredDate: "2024-1-1"}, {Timezone: "Pacific/Apia", OccurredDate: "2011-12-30"}, {Timezone: "UTC", OccurredDate: "2024-01-01", SourceDate: "2024-01-02"}, {Timezone: "UTC", OccurredDate: "2024-01-02"}} {
		o := base
		o.Timing = &terms
		if ValidateOperation(&o) == nil {
			t.Fatal("invalid/contradictory timing accepted", terms)
		}
	}
	o := base
	o.Kind = "deposit"
	o.Quantity = "0"
	o.InstrumentKey = ""
	o.Timing = &d.InvestmentOperationTiming{Timezone: "UTC", SettlementDate: "2024-01-02"}
	if ValidateOperation(&o) == nil {
		t.Fatal("cash settlement precision accepted")
	}
	const tenant = "11111111-1111-4111-8111-111111111111"
	const actor = "22222222-2222-4222-8222-222222222222"
	const ledger = "33333333-3333-4333-8333-333333333333"
	o = base
	o.LedgerID = ledger
	o.OccurredAt = time.Time{}
	o.SettlementAt = time.Time{}
	o.Timing = &d.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: "2025-01-01"}
	if _, e := NormalizeMutation(tenant, actor, Mutation{Action: "create", Operation: o, ExpectedLedgerVersion: 1, RequestKey: "future"}, day); e == nil {
		t.Fatal("date-only bypassed future-date check")
	}
}

func TestDateOnlyNormativeCorporateTradeCashOrder(t *testing.T) {
	fund := op("fund", "deposit", "20", 1)
	buy := op("buy", "buy", "10", 2)
	buy.Quantity = "1"
	buy.InstrumentKey = "stock"
	split := op("split", "split", "0", 4)
	split.Currency = ""
	split.InstrumentKey = "stock"
	split.SplitNew = "2"
	split.SplitOld = "1"
	split.OccurredAt = time.Time{}
	split.Timing = &d.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: "2024-01-02"}
	sell := op("sell", "sell", "20", 3)
	sell.Quantity = "2"
	sell.InstrumentKey = "stock"
	sell.OccurredAt = time.Time{}
	sell.Timing = &d.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: "2024-01-02"}
	fee := op("fee", "fee", "1", 5)
	fee.OccurredAt = time.Time{}
	fee.Timing = &d.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: "2024-01-02"}
	input := []d.InvestmentOperation{sell, fee, split, buy, fund}
	start := day.AddDate(0, 0, 1)
	end := start.Add(24*time.Hour - time.Microsecond)
	for _, cut := range []time.Time{start, end} {
		expected := "30"
		if cut.Equal(end) {
			expected = "29"
		}
		r, e := Replay(testLedger(), input, cut)
		if e != nil || r.Snapshot.Cash[0].Settled != expected || r.Snapshot.Positions[0].Quantity != "0" {
			t.Fatal("split must precede sale despite later sequence; fee must follow trades", r, e)
		}
		p, e := ReplayPortfolio([]d.InvestmentLedger{testLedger()}, input, cut)
		if e != nil || p.Ledgers["ledger"].Snapshot.Cash[0].Settled != expected {
			t.Fatal("connected replay order diverged", p, e)
		}
	}
}

func TestDateOnlyBatchCannotBypassFutureCheck(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const actor = "22222222-2222-4222-8222-222222222222"
	const ledger = "33333333-3333-4333-8333-333333333333"
	b := ImportBatch{ImportID: "44444444-4444-4444-8444-444444444444", PreviewHash: strings.Repeat("a", 64), ExpectedImportVersion: 1, RequestKey: "batch", ExpectedLedgerVersions: map[string]int64{ledger: 1}, Entries: []ImportBatchEntry{{Record: 1, ExpectedRowVersion: 1, ReviewReason: "Reviewed precision", Operation: d.InvestmentOperation{LedgerID: ledger, Kind: "deposit", Currency: "USD", Amount: "1", Timing: &d.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: "2199-01-01"}}}}}
	if _, e := NormalizeImportBatch(tenant, actor, b); e == nil {
		t.Fatal("date-only batch bypassed future check")
	}
	b.Entries[0].Operation.Timing = &d.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: "2000-01-01"}
	normalized, e := NormalizeImportBatch(tenant, actor, b)
	if e != nil || normalized.Entries[0].Operation.OccurredAt.Format(time.RFC3339Nano) != "2000-01-01T23:59:59.999999Z" {
		t.Fatal(normalized, e)
	}
}

func TestDateOnlyTodayIsNotFutureDespiteConditionalDayEnd(t *testing.T) {
	o := op("cash", "deposit", "1", 1)
	o.OccurredAt = time.Time{}
	o.Timing = &d.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: "2024-01-01"}
	if e := ValidateOperation(&o); e != nil {
		t.Fatal(e)
	}
	now := day.Add(12 * time.Hour)
	if !o.OccurredAt.After(now) || operationIsFuture(o, now) {
		t.Fatal("today's observation mistaken for a future event", o)
	}
	if !operationIsFuture(o, day.Add(-time.Hour)) {
		t.Fatal("tomorrow accepted")
	}
	o.Timing = nil
	if !operationIsFuture(o, now) {
		t.Fatal("precise future timestamp accepted")
	}
}
