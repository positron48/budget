package marketdata

import (
	"sort"
	"time"
)

// MappedHistoryRanges requests only reviewed listing periods. The padding is
// calendar evidence to help locate a prior close, not a trading-calendar claim.
func MappedHistoryRanges(m Mapping, scope Scope, first, last, at time.Time, open bool) []JobRequest {
	from := first.UTC().Truncate(24*time.Hour).AddDate(0, 0, -14)
	minimum := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	if from.Before(minimum) {
		from = minimum
	}
	to := at.UTC().Truncate(24 * time.Hour)
	if !open {
		end := last.UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)
		if end.Before(to) {
			to = end
		}
	}
	var ranges []JobRequest
	for _, p := range m.HistoryPeriods() {
		if p.Provider != scope.Provider || p.AccessScope != scope.AccessScope {
			continue
		}
		start, end := from, to
		lower := p.From.UTC().Truncate(time.Second)
		if lower.Before(p.From) {
			lower = lower.Add(time.Second)
		}
		if lower.After(start) {
			start = lower
		}
		if !p.To.IsZero() {
			upper := p.To.UTC().Truncate(time.Second)
			if upper.Equal(p.To) {
				upper = upper.Add(-time.Second)
			}
			if upper.Before(end) {
				end = upper
			}
		}
		if end.After(start) {
			ranges = append(ranges, JobRequest{InstrumentID: p.InstrumentID, Kind: "daily_candle", From: start, To: end})
		}
	}
	return ranges
}

// PlanHistoryGaps removes previously requested intervals, regardless of their
// result. Errors and empty answers stay explicit evidence, never full coverage.
// The bounded result can be persisted and the next pass resumes from those jobs.
func PlanHistoryGaps(want JobRequest, requested []JobRequest, limit int) ([]JobRequest, error) {
	if limit < 1 || limit > 100 || want.Kind != "daily_candle" || want.From.Year() < 1900 || want.To.Year() > 2200 || !want.To.After(want.From) || want.From.Nanosecond() != 0 || want.To.Nanosecond() != 0 {
		return nil, ErrInvalid
	}
	probe := want
	probe.To = probe.From.Add(time.Second)
	if ValidateRequest(probe) != nil {
		return nil, ErrInvalid
	}
	intervals := append([]JobRequest(nil), requested...)
	for _, q := range intervals {
		if q.InstrumentID != want.InstrumentID || q.Kind != want.Kind || ValidateRequest(q) != nil {
			return nil, ErrInvalid
		}
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].From.Before(intervals[j].From) })
	var result []JobRequest
	appendGap := func(start, end time.Time) {
		for start.Before(end) && len(result) < limit {
			next := start.Add(365 * 24 * time.Hour)
			if end.Before(next) {
				next = end
			}
			q := want
			q.From = start.UTC()
			q.To = next.UTC()
			result = append(result, q)
			start = next
		}
	}
	cursor := want.From
	for _, q := range intervals {
		if !q.To.After(cursor) || !q.From.Before(want.To) {
			continue
		}
		if q.From.After(cursor) {
			appendGap(cursor, q.From)
		}
		if q.To.After(cursor) {
			cursor = q.To
		}
		if !cursor.Before(want.To) || len(result) >= limit {
			return result, nil
		}
	}
	appendGap(cursor, want.To)
	return result, nil
}
