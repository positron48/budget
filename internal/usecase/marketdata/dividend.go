package marketdata

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// Dividend is an unchanged provider declaration, not a personal entitlement,
// gross-income fact, or evidence that the owner's account was credited.
type Dividend struct {
	DividendNet, ClosePrice                                       *BondAmount
	PaymentDate, DeclaredDate, LastBuyDate, RecordDate, CreatedAt time.Time
	DividendType, Regularity                                      string
	YieldValue                                                    *string
}
type DividendFacts struct {
	Instrument     Instrument
	From, To, AsOf time.Time // From/To filter RecordDate, never PaymentDate.
	Dividends      []Dividend
}
type SavedDividendFacts struct {
	DividendFacts
	ID, Hash   string
	RecordedAt time.Time
}

func NormalizeDividendFacts(f DividendFacts) (DividendFacts, error) {
	if ValidateInstrument(f.Instrument) != nil || !f.Instrument.Detailed || (f.Instrument.Kind != "share" && f.Instrument.Kind != "etf") || ValidateRequest(JobRequest{Kind: "dividend_facts", InstrumentID: f.Instrument.ID, From: f.From, To: f.To}) != nil || !validBondTime(f.AsOf, true) || len(f.Dividends) > 10000 {
		return f, ErrInvalid
	}
	seen := map[string]bool{}
	type entry struct {
		value Dividend
		key   string
	}
	entries := make([]entry, 0, len(f.Dividends))
	for _, d := range f.Dividends {
		if !validBondTime(d.RecordDate, true) || d.RecordDate.Before(f.From) || d.RecordDate.After(f.To) || !validBondTime(d.PaymentDate, false) || !validBondTime(d.DeclaredDate, false) || !validBondTime(d.LastBuyDate, false) || !validBondTime(d.CreatedAt, false) || !validBondAmount(d.DividendNet, false) || !validBondAmount(d.ClosePrice, false) || !validOptionalBondNumber(d.YieldValue) || !validBondText(d.DividendType, 200) || !validBondText(d.Regularity, 200) {
			return f, ErrInvalid
		}
		raw, err := json.Marshal(d)
		if err != nil || seen[string(raw)] {
			return f, ErrInvalid
		}
		seen[string(raw)] = true
		entries = append(entries, entry{d, string(raw)})
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].value.RecordDate.Equal(entries[j].value.RecordDate) {
			return entries[i].value.RecordDate.Before(entries[j].value.RecordDate)
		}
		return entries[i].key < entries[j].key
	})
	f.Dividends = make([]Dividend, len(entries))
	for i, v := range entries {
		f.Dividends[i] = v.value
	}
	return f, nil
}

type DividendProvider interface {
	LoadDividendFacts(context.Context, JobRequest) (DividendFacts, error)
}
type DividendQueue interface {
	CompleteDividendFacts(context.Context, Job, DividendFacts) error
}
type DividendReadRepo interface {
	DividendFacts(context.Context, Scope, JobRequest) (*SavedDividendFacts, error)
}

func (s *Service) DividendFacts(ctx context.Context, t, u string, scope Scope, q JobRequest) (*SavedDividendFacts, error) {
	if err := s.allow(ctx, t, u, scope); err != nil {
		return nil, err
	}
	if q.Kind != "dividend_facts" || ValidateRequest(q) != nil {
		return nil, ErrInvalid
	}
	r, ok := s.repo.(DividendReadRepo)
	if !ok {
		return nil, ErrNotFound
	}
	return r.DividendFacts(ctx, scope, q)
}
