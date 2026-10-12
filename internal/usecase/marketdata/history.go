package marketdata

import "time"

// PlanRequestedHistory accepts the public chart horizon, including an intraday
// final boundary. Worker requests retain their separate 365-day contract.
func PlanRequestedHistory(q JobRequest) ([]JobRequest, error) {
	if q.Kind != "daily_candle" || q.To.After(q.From.AddDate(30, 0, 0)) || q.SearchQuery != "" || q.InstrumentKind != "" {
		return nil, ErrInvalid
	}
	return PlanHistoryGaps(q, nil, 100)
}

// PlanHistory divides a half-open UTC range into provider-sized requests.
// Adjacent windows share the endpoint because providers may return it
// inclusively; observation identity makes that overlap idempotent.
// A successful request is evidence of observations, never of every session.
func PlanHistory(id string, from, to time.Time) ([]JobRequest, error) {
	if from.Location() != time.UTC || to.Location() != time.UTC ||
		from != from.Truncate(24*time.Hour) || to != to.Truncate(24*time.Hour) ||
		from.Year() < 1900 || to.Year() > 2200 || !to.After(from) {
		return nil, ErrInvalid
	}
	var result []JobRequest
	for start := from; start.Before(to); {
		end := start.Add(365 * 24 * time.Hour)
		if end.After(to) {
			end = to
		}
		q := JobRequest{InstrumentID: id, Kind: "daily_candle", From: start, To: end}
		if err := ValidateRequest(q); err != nil {
			return nil, err
		}
		result = append(result, q)
		if len(result) > 100 {
			return nil, ErrLimit
		}
		start = end
	}
	return result, nil
}
