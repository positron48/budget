package investment

import (
	"math/big"
	"slices"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

const forecastEventID = "00000000-0000-0000-0000-000000000099"

func forecastFixture() ([]d.InvestmentLedger, []d.InvestmentOperation, []ForecastSecurityLeg, ForecastEvent, ForecastQuery, time.Time) {
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ledgers := []d.InvestmentLedger{{ID: payoutLedgerID, TenantID: "tenant", StartedAt: at.AddDate(-2, 0, 0)}}
	makeOp := func(id, kind, qty string, when time.Time) d.InvestmentOperation {
		o := op(id, kind, "100", 1)
		o.LedgerID = payoutLedgerID
		o.InstrumentKey = "STOCK"
		o.Quantity = qty
		o.OccurredAt = when
		o.SettlementAt = when
		o.SettlementAssumed = false
		o.Version = 1
		if kind == "split" {
			o.Amount = "0"
			o.Quantity = "0"
			o.Currency = ""
			o.SplitNew = "2"
			o.SplitOld = "1"
		}
		return o
	}
	buy := makeOp("buy", "buy", "10", at.AddDate(0, -2, 0))
	buy.Sequence = 1
	split := makeOp("split", "split", "0", at.AddDate(0, -1, 0))
	split.Sequence = 2
	sell := makeOp("sell", "sell", "20", at.AddDate(0, 0, -1))
	sell.Sequence = 3
	ops := []d.InvestmentOperation{buy, split, sell}
	legs := []ForecastSecurityLeg{{payoutLedgerID, d.InvestmentSecurityLeg{OperationID: "buy", InstrumentKey: "STOCK", Quantity: "10", EffectiveAt: buy.OccurredAt}}, {payoutLedgerID, d.InvestmentSecurityLeg{OperationID: "split", InstrumentKey: "STOCK", Quantity: "10", EffectiveAt: split.OccurredAt}}, {payoutLedgerID, d.InvestmentSecurityLeg{OperationID: "sell", InstrumentKey: "STOCK", Quantity: "-20", EffectiveAt: sell.OccurredAt}}}
	event := ForecastEvent{ID: forecastEventID, Version: 1, InstrumentKey: "STOCK", Kind: "dividend", Nature: "announced", Currency: "USD", PerShare: "1.123456789012", ExDate: "2026-10-02", RecordDate: "2026-10-05", PaymentDate: "2026-11-10", Timezone: "Europe/Moscow", EntitlementRule: "settled_by_cutoff", EntitlementAt: at.AddDate(0, 0, -7), SourceReference: "Synthetic issuer announcement", Evidence: "Explicit fixture record cutoff and settlement rule", Reason: "Synthetic review"}
	return ledgers, ops, legs, event, ForecastQuery{FromMonth: "2026-10", Currency: "USD", TaxRate: "13"}, at
}

func TestForecastHistoricalSoldPositionSplitAndOptionalTax(t *testing.T) {
	ledgers, ops, legs, event, q, at := forecastFixture()
	// Closed/archived accounts can still own rights fixed before sale.
	ledgers[0].Archived = true
	report, e := BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil)
	if e != nil || len(report.Months) != 12 || len(report.Rows) != 1 {
		t.Fatal(e, report)
	}
	row := report.Rows[0]
	if row.Entitlement != "historical_right" || row.Status != "upcoming" {
		t.Fatal(row)
	}
	expectPayoutRat(t, row.Quantity, "20")
	expectPayoutRat(t, row.Gross, "22.46913578024")
	expectPayoutRat(t, row.EstimatedTax, "2.9209876514312")
	expectPayoutRat(t, row.EstimatedNet, "19.5481481288088")
	if report.MonthlyAverageGross.Cmp(new(big.Rat).Quo(row.Gross, big.NewRat(12, 1))) != 0 {
		t.Fatal("average rounded")
	}
	q.TaxRate = ""
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil)
	if e != nil || report.EstimatedNet != nil || report.Rows[0].EstimatedTax != nil || report.Rows[0].EstimatedNet != nil {
		t.Fatal("invented tax", e, report)
	}
}

func TestForecastSettlementAndPendingSplit(t *testing.T) {
	ledgers, ops, legs, event, q, at := forecastFixture()
	ops = ops[:2]
	legs = legs[:2]
	// A purchase traded before a split but settling after the record cutoff
	// contributes neither the original quantity nor its projected split delta.
	ops[0].SettlementAt = event.EntitlementAt.Add(time.Hour)
	report, e := BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil)
	if e != nil || len(report.Rows) != 0 {
		t.Fatal("pending split created rights", e, report.Rows)
	}
	ops[0].SettlementAt = event.EntitlementAt
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil)
	if e != nil || len(report.Rows) != 1 {
		t.Fatal(e, report)
	}
	expectPayoutRat(t, report.Rows[0].Quantity, "20")
	ops[0].SettlementAssumed = true
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil)
	if e != nil || len(report.Rows) != 1 || report.Rows[0].Gross != nil || !slices.Contains(report.Rows[0].Issues, "settlement_unconfirmed") || report.Gross != nil {
		t.Fatal("assumption certified", e, report)
	}
	ops[0].SettlementAssumed = false
	event.EntitlementRule = "trade_before_cutoff"
	event.EntitlementAt = ops[0].OccurredAt
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil)
	if e != nil || len(report.Rows) != 0 {
		t.Fatal("exclusive ex cutoff ignored", e, report)
	}
}

func TestForecastFutureScenarioUnknownPastCutoffAndFX(t *testing.T) {
	ledgers, ops, legs, event, q, at := forecastFixture()
	ops = ops[:2]
	legs = legs[:2]
	event.Nature = "forecast"
	event.EntitlementAt = at.AddDate(0, 0, 24)
	event.ExDate = ""
	event.RecordDate = ""
	q.Currency = "RUB"
	q.FXPolicy = "reviewed@1"
	points := []calc.FXFixing{{ID: "market-rate", Revision: 1, Policy: q.FXPolicy, From: "USD", To: "RUB", Rate: "90.123456789012", At: at.Add(-time.Hour), AvailableAt: at.Add(-time.Minute), ValidUntil: at.Add(time.Hour)}}
	report, e := BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, points)
	if e != nil || len(report.Rows) != 1 || report.Projected != 1 || report.Announced != 0 {
		t.Fatal(e, report)
	}
	row := report.Rows[0]
	if row.Entitlement != "current_composition_scenario" || len(row.FXLegs) != 1 || row.FXLegs[0].ID != "market-rate" {
		t.Fatal(row)
	}
	want := new(big.Rat).Mul(rational("22.46913578024"), rational("90.123456789012"))
	if row.ConvertedGross.Cmp(want) != 0 {
		t.Fatal("FX rounding", row.ConvertedGross, want)
	}
	// Stale evidence never becomes a future FX forecast or silently falls back.
	points[0].ValidUntil = at
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, points)
	if e != nil || report.Gross != nil || report.Unknown != 1 || report.Months[1].Gross != nil || report.Months[1].KnownGross.Sign() != 0 {
		t.Fatal("stale FX certified", e, report)
	}
	if report.Rows[0].Gross == nil || !slices.Contains(report.Rows[0].Issues, "current_fx_missing") {
		t.Fatal(report.Rows)
	}
	// A known past record date with unknown rights rule must not use today's
	// composition, even though the expected payment is still in the future.
	event.EntitlementRule = "unknown"
	event.EntitlementAt = time.Time{}
	event.RecordDate = "2026-10-01"
	q.Currency = "USD"
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil)
	if e != nil || report.Rows[0].Quantity != nil || report.Gross != nil || !slices.Contains(report.Rows[0].Issues, "past_entitlement_unknown") {
		t.Fatal(e, report)
	}
}

func TestForecastActualLinksStayFactsAndRequireReview(t *testing.T) {
	ledgers, ops, legs, event, q, at := forecastFixture()
	actual := op("credit", "dividend", "20", 4)
	actual.LedgerID = payoutLedgerID
	actual.InstrumentKey = "STOCK"
	actual.Tax = "2"
	actual.OccurredAt = at.Add(-time.Hour)
	actual.Version = 1
	ops = append(ops, actual)
	link := ForecastLink{EventID: event.ID, EventVersion: 1, LedgerID: payoutLedgerID, OperationID: actual.ID, OperationVersion: 1, Complete: true}
	report, e := BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, []ForecastLink{link}, q, at, nil)
	if e != nil || len(report.Rows) != 1 || report.Paid != 1 || report.Gross.Sign() != 0 || report.Rows[0].Status != "paid" {
		t.Fatal(e, report)
	}
	expectPayoutRat(t, report.Rows[0].ActualNet, "18")
	if report.Rows[0].EstimatedTax != nil {
		t.Fatal("forecast tax applied to fact")
	}
	link.Complete = false
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, []ForecastLink{link}, q, at, nil)
	if e != nil || report.Gross != nil || !slices.Contains(report.Rows[0].Issues, "partial_actual_payment") {
		t.Fatal("partial credit certified", e, report)
	}
	link.Complete = true
	ops[len(ops)-1].Version++
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, []ForecastLink{link}, q, at, nil)
	if e != nil || report.Paid != 0 || report.Gross != nil || !slices.Contains(report.Rows[0].Issues, "actual_link_requires_review") {
		t.Fatal("amended credit ignored", e, report)
	}
	if _, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, []ForecastLink{link, link}, q, at, nil); e == nil {
		t.Fatal("double-linked credit accepted")
	}
	link.Cancelled = true
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, []ForecastLink{link}, q, at, nil)
	if e != nil || report.Paid != 0 || report.Gross == nil || report.Rows[0].ActualNet != nil {
		t.Fatal("cancelled association remained active", e, report)
	}
	// Date passing alone never creates a fact.
	event.PaymentDate = "2026-10-10"
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil)
	if e != nil || report.Paid != 0 || report.Rows[0].Status != "overdue" || report.Rows[0].ActualNet != nil {
		t.Fatal("date posted cash", e, report)
	}
}

func TestForecastBoundariesMissingAmountAndValidation(t *testing.T) {
	ledgers, ops, legs, event, q, at := forecastFixture()
	event.PerShare = ""
	report, e := BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil)
	if e != nil || report.Gross != nil || report.MonthlyAverageGross != nil || report.Unknown != 1 || report.Months[1].Gross != nil {
		t.Fatal(e, report)
	}
	event.Cancelled = true
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil)
	if e != nil || len(report.Rows) != 0 || len(report.Months) != 12 {
		t.Fatal(e, report)
	}
	event.Cancelled = false
	event.PaymentDate = "2027-10-01"
	report, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil)
	if e != nil || len(report.Rows) != 0 {
		t.Fatal("exclusive horizon ignored", e, report)
	}
	for _, mutate := range []func(*ForecastEvent){func(e *ForecastEvent) { e.PerShare = "-1" }, func(e *ForecastEvent) { e.Evidence = "" }, func(e *ForecastEvent) { e.Timezone = "Local" }, func(e *ForecastEvent) { e.PaymentDate = "2026-02-30" }, func(e *ForecastEvent) { e.EntitlementRule = "guess-t2" }} {
		bad := event
		mutate(&bad)
		if _, e = NormalizeForecastEvent(bad); e == nil {
			t.Fatal("invalid event accepted", bad)
		}
	}
	q.TaxRate = "100.0000000001"
	if _, e = NormalizeForecastQuery(q); e == nil {
		t.Fatal("invalid tax accepted")
	}
	ledgers[0].TenantID = "foreign"
	q.TaxRate = "0"
	if _, e = BuildForecast("tenant", ledgers, ops, legs, []ForecastEvent{event}, nil, q, at, nil); e != d.ErrInvestmentForbidden {
		t.Fatal("tenant leak", e)
	}
}
