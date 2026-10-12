package marketdata

import (
	"testing"
	"time"
)

func retrospectiveFixture() (PriceSelectionRequest, []ScopedObservation) {
	q, v := selectionFixture()
	q.Calendar = nil
	q.Retrospective = &RetrospectiveDailyPolicy{Convention: RetrospectiveCloseNextUTCDay, MaxAge: 96 * time.Hour}
	return q, v
}

func TestRetrospectiveCloseEligibilityAndExpiry(t *testing.T) {
	q, v := retrospectiveFixture()
	eligible := v[0].At.Add(24 * time.Hour)
	for _, tc := range []struct {
		at        time.Time
		state, id string
	}{
		{eligible.Add(-time.Nanosecond), "unavailable", ""},
		{eligible, "fresh", "friday"},
		{eligible.Add(48 * time.Hour), "fresh", "friday"},
		{eligible.Add(96*time.Hour - time.Nanosecond), "fresh", "friday"},
		{eligible.Add(96 * time.Hour), "stale", "friday"},
	} {
		q.At = tc.at
		got, err := SelectValuationPrice(q, v[:1])
		if err != nil || got.State != tc.state || got.ObservationID != tc.id || got.TimeConvention != RetrospectiveCloseNextUTCDay || got.CalendarID != "" || got.CalendarRevision != 0 {
			t.Fatal(tc, got, err)
		}
		if got.Fact != nil && (!got.Fact.EffectiveAt.Equal(eligible) || !got.Fact.AvailableAt.Equal(eligible)) {
			t.Fatal("unexpected availability", got)
		}
		if tc.state != "fresh" && got.Fact != nil {
			t.Fatal("unusable price became a fact", got)
		}
	}
	q.At = v[1].At.Add(24 * time.Hour)
	got, err := SelectValuationPrice(q, v)
	if err != nil || got.Fact == nil || got.ObservationID != "monday" {
		t.Fatal(got, err)
	}
}

func TestRetrospectiveIncompleteCorrectionAndKnowledgeCutoff(t *testing.T) {
	q, v := retrospectiveFixture()
	q.At = v[1].At.Add(24 * time.Hour)
	corrected := v[1]
	corrected.Revision = 2
	corrected.ReceivedAt = corrected.ReceivedAt.Add(2 * time.Hour)
	corrected.Complete = false
	v = append(v, corrected)
	got, err := SelectValuationPrice(q, v)
	if err != nil || got.State != "partial" || got.Fact != nil || got.Candidate == nil || len(got.Issues) != 1 || got.Issues[0] != "retrospective_latest_candle_incomplete" {
		t.Fatal(got, err)
	}
	q.KnowledgeAt = v[1].ReceivedAt
	got, err = SelectValuationPrice(q, v)
	if err != nil || got.State != "fresh" || got.ObservationID != "monday" || got.ObservationRevision != 1 {
		t.Fatal(got, err)
	}
	// An incomplete future candle must not invalidate the earlier eligible close.
	q.KnowledgeAt = time.Time{}
	q.At = v[1].At
	got, err = SelectValuationPrice(q, v)
	if err != nil || got.State != "fresh" || got.ObservationID != "friday" {
		t.Fatal(got, err)
	}
}

func TestRetrospectivePolicyRequiresExplicitBoundedOptIn(t *testing.T) {
	for _, mutate := range []func(*PriceSelectionRequest){
		func(q *PriceSelectionRequest) { q.Calendar = &SessionCalendar{} },
		func(q *PriceSelectionRequest) { q.Retrospective.Convention = "inferred_calendar" },
		func(q *PriceSelectionRequest) { q.Retrospective.MaxAge = 0 },
		func(q *PriceSelectionRequest) { q.Retrospective.MaxAge = 31 * 24 * time.Hour },
		func(q *PriceSelectionRequest) { q.Kind = "last_price" },
		func(q *PriceSelectionRequest) { q.SourceKind = "dealer" },
		func(q *PriceSelectionRequest) { q.MaxQuoteAge = time.Hour },
	} {
		q, v := retrospectiveFixture()
		mutate(&q)
		if _, err := SelectValuationPrice(q, v); err == nil {
			t.Fatal("invalid retrospective policy accepted", q)
		}
	}
	q, v := retrospectiveFixture()
	v[0].At = v[0].At.Add(time.Hour)
	if _, err := SelectValuationPrice(q, v); err == nil {
		t.Fatal("non-UTC-day candle accepted")
	}
	q, v = retrospectiveFixture()
	q.Retrospective = nil
	got, err := SelectValuationPrice(q, v)
	if err != nil || got.Fact != nil || got.Issues[0] != "calendar_missing" {
		t.Fatal("strict default changed", got, err)
	}
	q, v = retrospectiveFixture()
	q.At = v[0].At.Add(24 * time.Hour)
	q.Adjustment = "provider_unspecified"
	for i := range v {
		v[i].Adjustment = q.Adjustment
	}
	got, err = SelectValuationPrice(q, v)
	if err != nil || got.Fact == nil || got.Fact.Adjustment != "provider_unspecified" {
		t.Fatal("unknown adjustment inferred", got, err)
	}
}
