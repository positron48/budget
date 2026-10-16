package marketdata

import (
	"sort"
	"time"
)

// PriceSeries owns a normalized copy of one entitled source series. Repeated
// retrospective dates still run all revision, eligibility and quality rules;
// only decimal parsing is shared. No unvalidated input can enter this path.
type PriceSeries struct {
	identity     PriceSelectionRequest
	observations []ScopedObservation
}

func NewPriceSeries(q PriceSelectionRequest, input []ScopedObservation) (*PriceSeries, error) {
	s := &PriceSeries{identity: q, observations: make([]ScopedObservation, len(input))}
	for i, v := range input {
		o, e := Normalize(v.Observation)
		if e != nil {
			return nil, e
		}
		v.Observation = o
		s.observations[i] = v
	}
	if _, e := selectValuationPrice(q, s.observations, true); e != nil {
		return nil, e
	}
	sort.SliceStable(s.observations, func(i, j int) bool { return s.observations[i].At.Before(s.observations[j].At) })
	return s, nil
}

func (s *PriceSeries) Select(q PriceSelectionRequest) (SelectedPrice, error) {
	if s == nil || s.identity.Scope != q.Scope || s.identity.InstrumentID != q.InstrumentID || s.identity.Kind != q.Kind || s.identity.SourceKind != q.SourceKind || s.identity.Adjustment != q.Adjustment || !s.identity.KnowledgeAt.Equal(q.KnowledgeAt) {
		return SelectedPrice{}, ErrInvalid
	}
	return selectValuationPrice(q, s.observations, true)
}

// SelectSince keeps diagnostic quality independent of the outer batch window.
// It shares normalized observations while applying a fixed per-date lookback.
func (s *PriceSeries) SelectSince(q PriceSelectionRequest, from time.Time) (SelectedPrice, error) {
	if s == nil || s.identity.Scope != q.Scope || s.identity.InstrumentID != q.InstrumentID || s.identity.Kind != q.Kind || s.identity.SourceKind != q.SourceKind || s.identity.Adjustment != q.Adjustment || !s.identity.KnowledgeAt.Equal(q.KnowledgeAt) || from.After(q.At) {
		return SelectedPrice{}, ErrInvalid
	}
	first := sort.Search(len(s.observations), func(i int) bool { return !s.observations[i].At.Before(from) })
	end := sort.Search(len(s.observations), func(i int) bool { return s.observations[i].At.After(q.At) })
	return selectValuationPrice(q, s.observations[first:end], true)
}
