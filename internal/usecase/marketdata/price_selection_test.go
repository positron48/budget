package marketdata

import (
	"testing"
	"time"
)

func selectionFixture() (PriceSelectionRequest, []ScopedObservation) {
	const id = "11111111-1111-4111-8111-111111111111"
	scope := Scope{TenantID: id, Provider: "tinvest", AccessScope: "personal"}
	friday := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	monday := friday.AddDate(0, 0, 3)
	calendar := &SessionCalendar{Scope: scope, InstrumentID: id, SourceKind: "exchange", ID: "verified-calendar", Revision: 2, From: friday, Through: monday.Add(24 * time.Hour), Sessions: []TradingSession{{friday, friday.Add(18 * time.Hour), friday.Add(18*time.Hour + 15*time.Minute)}, {monday, monday.Add(18 * time.Hour), monday.Add(18*time.Hour + 15*time.Minute)}}}
	q := PriceSelectionRequest{Scope: scope, InstrumentID: id, Kind: "daily_candle", SourceKind: "exchange", Adjustment: "unadjusted", At: friday.Add(19 * time.Hour), Calendar: calendar}
	observation := func(at time.Time, price, id string) ScopedObservation {
		return ScopedObservation{Scope: scope, SavedObservation: SavedObservation{ID: id, Revision: 1, ReceivedAt: monday.Add(23 * time.Hour), Observation: Observation{InstrumentID: q.InstrumentID, Kind: q.Kind, At: at, SourceKind: q.SourceKind, Currency: "RUB", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Price: price, Open: price, Low: price, High: price, Volume: "1", Complete: true}}}
	}
	return q, []ScopedObservation{observation(friday, "100", "friday"), observation(monday, "110", "monday")}
}
func TestPriceSelectionNoFutureCloseAndWeekendCarry(t *testing.T) {
	q, values := selectionFixture()
	q.At = q.Calendar.Sessions[0].CloseAt
	got, e := SelectValuationPrice(q, values)
	if e != nil || got.Fact != nil || got.State != "unavailable" {
		t.Fatal(got, e)
	}
	q.At = q.Calendar.Sessions[0].AvailableAt
	got, e = SelectValuationPrice(q, values)
	if e != nil || got.Fact == nil || got.Fact.Price != "100.000000000" || !got.Fact.EffectiveAt.Equal(q.Calendar.Sessions[0].CloseAt) || got.CalendarRevision != 2 {
		t.Fatal(got, e)
	}
	q.At = q.Calendar.Sessions[0].CloseAt.Add(48 * time.Hour) // Sunday: verified no expected session.
	got, e = SelectValuationPrice(q, values)
	if e != nil || got.State != "fresh" || got.ObservationID != "friday" {
		t.Fatal(got, e)
	}
	q.At = q.Calendar.Sessions[1].AvailableAt
	got, e = SelectValuationPrice(q, values)
	if e != nil || got.ObservationID != "monday" {
		t.Fatal(got, e)
	}
}
func TestPriceSelectionMissingOrIncompleteExpectedSessionIsStale(t *testing.T) {
	q, values := selectionFixture()
	q.At = q.Calendar.Sessions[1].AvailableAt
	for _, input := range [][]ScopedObservation{values[:1], values} {
		if len(input) == 2 {
			input[1].Complete = false
		}
		got, e := SelectValuationPrice(q, input)
		if e != nil || got.State != "stale" || got.Fact != nil || got.Candidate == nil || got.ObservationID != "friday" {
			t.Fatal(got, e)
		}
	}
}
func TestPriceSelectionCalendarAndScopeEvidence(t *testing.T) {
	q, values := selectionFixture()
	noCalendar := q
	noCalendar.Calendar = nil
	got, e := SelectValuationPrice(noCalendar, values)
	if e != nil || got.Fact != nil || got.Issues[0] != "calendar_missing" {
		t.Fatal(got, e)
	}
	q.At = q.Calendar.Through.Add(time.Second)
	got, e = SelectValuationPrice(q, values)
	if e != nil || got.Fact != nil || got.Issues[0] != "calendar_coverage_missing" {
		t.Fatal(got, e)
	}
	q.At = q.Calendar.Sessions[0].AvailableAt
	q.Calendar.InstrumentID = "22222222-2222-4222-8222-222222222222"
	if _, e = SelectValuationPrice(q, values); e == nil {
		t.Fatal("foreign calendar accepted")
	}
	q, values = selectionFixture()
	values[0].AccessScope = "another"
	if _, e = SelectValuationPrice(q, values); e == nil {
		t.Fatal("foreign source evidence accepted")
	}
	q, values = selectionFixture()
	values[0].SourceKind = "dealer"
	if _, e = SelectValuationPrice(q, values); e == nil {
		t.Fatal("dealer silently used")
	}
}
func TestPriceSelectionCorrectionAndKnowledgeCutoff(t *testing.T) {
	q, values := selectionFixture()
	values = values[:1]
	corrected := values[0]
	corrected.Revision = 2
	corrected.Price = "101"
	corrected.Open = "101"
	corrected.High = "101"
	corrected.Low = "101"
	corrected.ReceivedAt = corrected.ReceivedAt.Add(time.Hour)
	values = append(values, corrected)
	got, e := SelectValuationPrice(q, values)
	if e != nil || got.ObservationRevision != 2 || got.Fact.Price != "101.000000000" {
		t.Fatal(got, e)
	}
	q.KnowledgeAt = values[0].ReceivedAt
	got, e = SelectValuationPrice(q, values)
	if e != nil || got.ObservationRevision != 1 || got.Fact.Price != "100.000000000" {
		t.Fatal(got, e)
	}
	q.KnowledgeAt = q.KnowledgeAt.Add(-time.Second)
	got, e = SelectValuationPrice(q, values)
	if e != nil || got.Fact != nil {
		t.Fatal(got, e)
	}
	q.KnowledgeAt = time.Time{}
	values[1].Complete = false
	got, e = SelectValuationPrice(q, values)
	if e != nil || got.Fact != nil {
		t.Fatal("old revision hid incomplete correction", got, e)
	}
}
func TestPriceSelectionUnknownSessionAndAmbiguity(t *testing.T) {
	q, values := selectionFixture()
	unknown := values[0]
	unknown.ID = "unknown"
	unknown.At = unknown.At.Add(time.Hour)
	got, e := SelectValuationPrice(q, append(values, unknown))
	if e != nil || got.State != "partial" || got.Fact != nil || got.Candidate == nil {
		t.Fatal(got, e)
	}
	ambiguous := values[0]
	ambiguous.ID = "ambiguous"
	if _, e = SelectValuationPrice(q, append(values, ambiguous)); e == nil {
		t.Fatal("ambiguous series accepted")
	}
	conflict := values[0]
	conflict.Price = "99"
	conflict.Open = "99"
	conflict.High = "99"
	conflict.Low = "99"
	if _, e = SelectValuationPrice(q, append(values, conflict)); e == nil {
		t.Fatal("conflicting revision accepted")
	}
}
func TestPriceSelectionQuoteAgeAndUnresolvedAdjustment(t *testing.T) {
	q, values := selectionFixture()
	q.Kind = "last_price"
	q.Calendar = nil
	q.MaxQuoteAge = time.Minute
	v := values[0]
	v.Kind = q.Kind
	v.At = q.At.Add(-time.Minute)
	v.Open = ""
	v.High = ""
	v.Low = ""
	v.Volume = ""
	v.Adjustment = "provider_unspecified"
	q.Adjustment = v.Adjustment
	got, e := SelectValuationPrice(q, []ScopedObservation{v})
	if e != nil || got.Fact == nil || got.Fact.Adjustment != "provider_unspecified" {
		t.Fatal(got, e)
	}
	q.At = q.At.Add(time.Nanosecond)
	got, e = SelectValuationPrice(q, []ScopedObservation{v})
	if e != nil || got.Fact != nil || got.State != "stale" {
		t.Fatal(got, e)
	}
	q.MaxQuoteAge = 0
	if _, e = SelectValuationPrice(q, []ScopedObservation{v}); e == nil {
		t.Fatal("implicit age policy accepted")
	}
}
