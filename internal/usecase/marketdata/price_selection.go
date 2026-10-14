package marketdata

import (
	"fmt"
	"time"

	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

// TradingSession is verified calendar evidence. ObservationAt is the provider's
// candle identity; CloseAt is economic close; AvailableAt includes publication
// delay. These timestamps must not be inferred by adding 24h to an API candle.
type TradingSession struct{ ObservationAt, CloseAt, AvailableAt time.Time }
type SessionCalendar struct {
	Scope
	InstrumentID, SourceKind string
	ID                       string
	Revision                 int64
	From, Through            time.Time // verified coverage, inclusive
	Sessions                 []TradingSession
}

const RetrospectiveCloseNextUTCDay = "retrospective_close_next_utc_day_v1"

// RetrospectiveDailyPolicy is a valuation rule, not verified session/publication
// evidence. It never supplies a calendar or promises historical tradability.
type RetrospectiveDailyPolicy struct {
	Convention string
	MaxAge     time.Duration
}

type ScopedObservation struct {
	Scope
	SavedObservation
}
type PriceSelectionRequest struct {
	Scope
	InstrumentID, Kind, SourceKind, Adjustment string
	At                                         time.Time
	// Optional point-in-time reconstruction: only revisions received by this
	// timestamp may be used. Empty means corrected history as currently known.
	KnowledgeAt   time.Time
	Calendar      *SessionCalendar          // required for daily candles unless Retrospective is explicit
	Retrospective *RetrospectiveDailyPolicy `json:",omitempty"`
	MaxQuoteAge   time.Duration             // required for last price; explicit elapsed-time SLA
}
type SelectedPrice struct {
	AdjustmentProof     *calc.PriceAdjustmentProof `json:",omitempty"`
	TimeConvention      string                     `json:",omitempty"`
	Fact                *calc.PriceFact            // only supplied when selection quality permits valuation
	Candidate           *calc.PriceFact            // may be stale; for diagnostics/display, never fallback NAV
	ObservationID       string
	ObservationRevision int64
	ReceivedAt          time.Time
	CalendarID          string
	CalendarRevision    int64
	State               string // fresh, stale, partial, unavailable
	Issues              []string
}

// SelectValuationPrice chooses one source/listing/series and retains provenance.
// It never mixes exchange/dealer feeds or treats unknown adjustment as raw.
// Unit/adjustment validation remains in NormalizePrice after this time selection.
func SelectValuationPrice(q PriceSelectionRequest, input []ScopedObservation) (SelectedPrice, error) {
	out := SelectedPrice{State: "unavailable", Issues: []string{}}
	if (q.Adjustment != "unadjusted" && q.Adjustment != "provider_unspecified") || ValidateScope(q.Scope) != nil || q.At.IsZero() || (q.Kind != "last_price" && q.Kind != "daily_candle") || (q.SourceKind != "exchange" && q.SourceKind != "dealer") {
		return out, ErrInvalid
	}
	// Reuse the existing canonical identifier validation.
	if ValidateRequest(JobRequest{InstrumentID: q.InstrumentID, Kind: "last_price"}) != nil {
		return out, ErrInvalid
	}
	if q.Retrospective != nil {
		r := q.Retrospective
		if q.Kind != "daily_candle" || q.SourceKind != "exchange" || q.Calendar != nil || q.MaxQuoteAge != 0 || r.Convention != RetrospectiveCloseNextUTCDay || r.MaxAge <= 0 || r.MaxAge > 30*24*time.Hour {
			return out, ErrInvalid
		}
		out.TimeConvention = r.Convention
	}
	sessions := map[time.Time]TradingSession{}
	var latest *TradingSession
	if q.Kind == "daily_candle" && q.Retrospective == nil {
		c := q.Calendar
		if c == nil {
			out.Issues = append(out.Issues, "calendar_missing")
			return out, nil
		}
		if c.Scope != q.Scope || c.InstrumentID != q.InstrumentID || c.SourceKind != q.SourceKind || c.ID == "" || c.Revision < 1 || c.From.IsZero() || c.Through.Before(c.From) {
			return out, ErrInvalid
		}
		out.CalendarID = c.ID
		out.CalendarRevision = c.Revision
		if q.At.Before(c.From) || q.At.After(c.Through) {
			out.Issues = append(out.Issues, "calendar_coverage_missing")
			return out, nil
		}
		var previous TradingSession
		for i, s := range c.Sessions {
			if s.ObservationAt.IsZero() || !s.CloseAt.After(s.ObservationAt) || s.AvailableAt.Before(s.CloseAt) || s.ObservationAt.Before(c.From) || s.AvailableAt.After(c.Through) || (i > 0 && (!s.ObservationAt.After(previous.ObservationAt) || s.ObservationAt.Before(previous.CloseAt) || s.AvailableAt.Before(previous.AvailableAt))) {
				return out, ErrInvalid
			}
			sessions[s.ObservationAt.UTC()] = s
			if !s.AvailableAt.After(q.At) {
				copy := s
				latest = &copy
			}
			previous = s
		}
	} else if q.Kind == "last_price" && (q.MaxQuoteAge <= 0 || q.MaxQuoteAge > 30*24*time.Hour) {
		return out, ErrInvalid
	}

	// The storage reader may supply revision history for a knowledge cutoff. Keep
	// the greatest eligible revision, never an older complete copy of a correction.
	revisions := map[string]ScopedObservation{}
	for _, v := range input {
		if v.Scope != q.Scope || v.InstrumentID != q.InstrumentID || v.Kind != q.Kind || v.SourceKind != q.SourceKind || v.Adjustment != q.Adjustment {
			return out, fmt.Errorf("%w: mixed price evidence", ErrInvalid)
		}
		if v.ID == "" || v.Revision < 1 || v.ReceivedAt.IsZero() {
			return out, ErrInvalid
		}
		normalized, err := Normalize(v.Observation)
		if err != nil {
			return out, err
		}
		v.Observation = normalized
		if !q.KnowledgeAt.IsZero() && v.ReceivedAt.After(q.KnowledgeAt) {
			continue
		}
		old, ok := revisions[v.ID]
		if ok && !old.At.Equal(v.At) {
			return out, fmt.Errorf("%w: observation identity changed", ErrInvalid)
		}
		if ok && old.Revision == v.Revision && (old.Observation != v.Observation || !old.ReceivedAt.Equal(v.ReceivedAt)) {
			return out, fmt.Errorf("%w: conflicting observation revision", ErrInvalid)
		}
		if !ok || v.Revision > old.Revision {
			revisions[v.ID] = v
		}
	}
	var best *ScopedObservation
	var bestFact *calc.PriceFact
	var latestIncomplete time.Time
	seenTimes := map[time.Time]string{}
	for _, v := range revisions {
		if other, ok := seenTimes[v.At]; ok && other != v.ID {
			return out, fmt.Errorf("%w: ambiguous price series", ErrInvalid)
		}
		seenTimes[v.At] = v.ID
		effective, available := v.At, v.At
		if q.Retrospective != nil {
			if !v.At.Equal(v.At.UTC().Truncate(24 * time.Hour)) {
				return out, ErrInvalid
			}
			effective = v.At.Add(24 * time.Hour)
			available = effective
			if !v.Complete && !effective.After(q.At) && effective.After(latestIncomplete) {
				latestIncomplete = effective
			}
		} else if q.Kind == "daily_candle" {
			s, ok := sessions[v.At.UTC()]
			if !ok {
				if !v.At.After(q.At) && !v.At.Before(q.Calendar.From) {
					out.Issues = append(out.Issues, "candle_session_unknown")
				}
				continue
			}
			effective, available = s.CloseAt, s.AvailableAt
		}
		if effective.After(q.At) || available.After(q.At) || !v.Complete {
			continue
		}
		if bestFact == nil || effective.After(bestFact.EffectiveAt) {
			copy := v
			best = &copy
			bestFact = &calc.PriceFact{InstrumentID: v.InstrumentID, SourceKey: q.Provider + "/" + q.AccessScope, Price: v.Price, Currency: v.Currency, Unit: v.PriceUnit, Adjustment: v.Adjustment, EffectiveAt: effective, AvailableAt: available, Complete: v.Complete}
		}
	}
	if best == nil {
		out.Issues = append(out.Issues, "price_unavailable")
		return out, nil
	}
	out.Candidate = bestFact
	out.ObservationID = best.ID
	out.ObservationRevision = best.Revision
	out.ReceivedAt = best.ReceivedAt
	if best.QualityIssue != "" {
		out.State = "unavailable"
		out.Issues = append(out.Issues, best.QualityIssue)
		return out, nil
	}
	stale := q.Kind == "last_price" && q.At.Sub(bestFact.EffectiveAt) > q.MaxQuoteAge
	if q.Retrospective != nil {
		stale = !q.At.Before(bestFact.EffectiveAt.Add(q.Retrospective.MaxAge))
		if latestIncomplete.After(bestFact.EffectiveAt) {
			out.Issues = append(out.Issues, "retrospective_latest_candle_incomplete")
		}
	} else if q.Kind == "daily_candle" {
		stale = latest != nil && bestFact.EffectiveAt.Before(latest.CloseAt)
	}
	if stale {
		out.State = "stale"
		out.Issues = append(out.Issues, "price_stale")
		return out, nil
	}
	if len(out.Issues) > 0 {
		out.State = "partial"
		return out, nil
	}
	out.State = "fresh"
	out.Fact = bestFact
	return out, nil
}
