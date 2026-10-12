package main

import "time"

// Summary counts observations, not confirmed mappings or complete date coverage.
type Summary struct {
	CashCurrencies          int       `json:"cash_currencies"`
	Securities              int       `json:"source_securities"`
	Instruments             int       `json:"source_instruments"`
	Pending                 int       `json:"pending_or_failed"`
	NoCandidate             int       `json:"no_exact_candidate"`
	MultipleCandidates      int       `json:"multiple_candidates"`
	Candidates              int       `json:"candidate_listings"`
	WithPrice               int       `json:"instruments_with_any_price"`
	WithSampleHistory       int       `json:"instruments_with_any_sample_history"`
	WithBothOnSameCandidate int       `json:"instruments_with_price_and_sample_on_same_candidate"`
	LatestPrice             time.Time `json:"latest_observed_price"`
	OldestPrice             time.Time `json:"oldest_observed_price"`
}

func summarize(r report) Summary {
	s := Summary{Instruments: len(r.Entries)}
	for _, e := range r.Entries {
		if e.Source.CashOnly {
			s.CashCurrencies++
			continue
		}
		s.Securities++
		if !e.Complete {
			s.Pending++
			continue
		}
		if len(e.Candidates) == 0 {
			s.NoCandidate++
		}
		if len(e.Candidates) > 1 {
			s.MultipleCandidates++
		}
		price, history, both := false, false, false
		for _, c := range e.Candidates {
			s.Candidates++
			hasPrice := len(c.Prices) > 0
			hasHistory := len(c.Candles) > 0
			price = price || hasPrice
			history = history || hasHistory
			both = both || (hasPrice && hasHistory)
			for _, p := range c.Prices {
				if s.LatestPrice.IsZero() || p.Time.After(s.LatestPrice) {
					s.LatestPrice = p.Time
				}
				if s.OldestPrice.IsZero() || p.Time.Before(s.OldestPrice) {
					s.OldestPrice = p.Time
				}
			}
		}
		if price {
			s.WithPrice++
		}
		if history {
			s.WithSampleHistory++
		}
		if both {
			s.WithBothOnSameCandidate++
		}
	}
	return s
}
