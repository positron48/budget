package marketdata

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
	return s, nil
}

func (s *PriceSeries) Select(q PriceSelectionRequest) (SelectedPrice, error) {
	if s == nil || s.identity.Scope != q.Scope || s.identity.InstrumentID != q.InstrumentID || s.identity.Kind != q.Kind || s.identity.SourceKind != q.SourceKind || s.identity.Adjustment != q.Adjustment || !s.identity.KnowledgeAt.Equal(q.KnowledgeAt) {
		return SelectedPrice{}, ErrInvalid
	}
	return selectValuationPrice(q, s.observations, true)
}
