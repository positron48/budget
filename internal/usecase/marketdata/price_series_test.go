package marketdata

import (
	"reflect"
	"testing"
	"time"
)

func TestPreparedPriceSeriesMatchesOrdinarySelection(t *testing.T) {
	for _, variant := range []string{"normal", "incomplete", "quality", "corrected", "knowledge"} {
		t.Run(variant, func(t *testing.T) {
			q, values := selectionFixture()
			q.Calendar = nil
			q.Retrospective = &RetrospectiveDailyPolicy{Convention: RetrospectiveCloseNextUTCDay, MaxAge: 96 * time.Hour}
			switch variant {
			case "incomplete":
				values[1].Complete = false
			case "quality":
				values[1].Open = "120"
				values[1].QualityIssue = "provider_ohlc_conflict"
			case "corrected", "knowledge":
				correction := values[1]
				correction.Revision = 2
				correction.ReceivedAt = correction.ReceivedAt.Add(24 * time.Hour)
				correction.Price, correction.Open, correction.Low, correction.High = "90", "90", "90", "90"
				values = append(values, correction)
				if variant == "knowledge" {
					q.KnowledgeAt = values[1].ReceivedAt
				}
			}
			series, e := NewPriceSeries(q, values)
			if e != nil {
				t.Fatal(e)
			}
			for day := -1; day < 12; day++ {
				q.At = values[0].At.AddDate(0, 0, day).Add(21 * time.Hour)
				want, e := SelectValuationPrice(q, values)
				if e != nil {
					t.Fatal(e)
				}
				got, e := series.Select(q)
				if e != nil || !reflect.DeepEqual(got, want) {
					t.Fatal(day, got, want, e)
				}
			}
			// Prepared evidence owns its copy; external edits cannot change a read.
			before, e := series.Select(q)
			if e != nil {
				t.Fatal(e)
			}
			values[0].Price = "99999"
			after, e := series.Select(q)
			if e != nil || !reflect.DeepEqual(before, after) {
				t.Fatal(after, e)
			}
			q.AccessScope = "other"
			if _, e = series.Select(q); e == nil {
				t.Fatal("cross-scope evidence accepted")
			}
		})
	}
}

func TestPreparedPriceSeriesRejectsMalformedEvidence(t *testing.T) {
	q, values := selectionFixture()
	values[0].Price = "NaN"
	if _, e := NewPriceSeries(q, values); e == nil {
		t.Fatal("invalid price accepted")
	}
	q, values = selectionFixture()
	values[1].ID = values[0].ID
	if _, e := NewPriceSeries(q, values); e == nil {
		t.Fatal("changed observation identity accepted")
	}
}
