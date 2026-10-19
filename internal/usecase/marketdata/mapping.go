package marketdata

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

var ErrConflict = errors.New("market mapping version conflict")

// MappingPeriod is explicit reviewed evidence, never inferred from ticker search.
// Periods are ordered and disjoint. Gaps deliberately mean no confirmed mapping.
type MappingPeriod struct {
	Provider, AccessScope, InstrumentID string
	Kind, Currency, Unit                string
	Nominal, NominalCurrency, LotSize   string
	From, To                            time.Time
	Evidence                            string
	// Canonical identity for regenerating licensed native caches after an
	// archive restore. A hashed provider ID alone cannot recover its symbol.
	NativeInstrument *Instrument `json:",omitempty"`
}

type Mapping struct {
	InstrumentKey     string
	Version           int64
	Periods           []MappingPeriod
	HistoricalPeriods []MappingPeriod `json:",omitempty"`
	PriceEstimates    []PriceEstimate `json:",omitempty"`
	ActorID, Reason   string
	RecordedAt        time.Time
}

// A dated, explicitly reviewed clean price per security, independent of an
// exchange listing. From is the first valuation instant where it may be used;
// At retains the actual age of the price instead of pretending it is fresh.
type PriceEstimate struct {
	Price, Currency, Reference string
	At, From                   time.Time
	Fallback                   bool `json:",omitempty"`
}

type MappingChange struct {
	InstrumentKey      string
	ExpectedVersion    int64
	RequestKey, Reason string
	Periods            []MappingPeriod
	// Nil preserves the historical schedule for existing API clients; an
	// explicit empty schedule clears it. The pointer also fences idempotency.
	HistoricalPeriods *[]MappingPeriod `json:",omitempty"`
	PriceEstimates    *[]PriceEstimate `json:",omitempty"`
}

type MappingRepo interface {
	SaveMapping(context.Context, string, string, MappingChange) (Mapping, error)
	GetMapping(context.Context, string, string) (Mapping, error)
}

type MappingService struct {
	repo  MappingRepo
	roles Roles
}

func NewMappingService(repo MappingRepo, roles Roles) *MappingService {
	return &MappingService{repo, roles}
}

func (s *MappingService) authorize(ctx context.Context, t, u string, write bool) error {
	for _, v := range []string{t, u} {
		if id, e := uuid.Parse(v); e != nil || id.String() != v {
			return ErrForbidden
		}
	}
	role, e := s.roles.GetUserRole(ctx, t, u)
	if e != nil {
		return ErrForbidden
	}
	if role == d.TenantRoleOwner || role == d.TenantRoleAdmin || (!write && role == d.TenantRoleMember) {
		return nil
	}
	return ErrForbidden
}

func (s *MappingService) Save(ctx context.Context, t, u string, c MappingChange) (Mapping, error) {
	if e := s.authorize(ctx, t, u, true); e != nil {
		return Mapping{}, e
	}
	if e := ValidateMappingChange(t, c); e != nil {
		return Mapping{}, e
	}
	return s.repo.SaveMapping(ctx, t, u, c)
}

func (s *MappingService) Get(ctx context.Context, t, u, key string) (Mapping, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return Mapping{}, e
	}
	if !mappingText(key, 200) {
		return Mapping{}, ErrInvalid
	}
	return s.repo.GetMapping(ctx, t, key)
}

func mappingText(s string, max int) bool {
	return len(s) > 0 && len(s) <= max && strings.TrimSpace(s) == s && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func ValidateMappingChange(tenant string, c MappingChange) error {
	if !mappingText(c.InstrumentKey, 200) || !mappingText(c.RequestKey, 128) || !mappingText(c.Reason, 2000) || c.ExpectedVersion < 0 || len(c.Periods) > 100 {
		return ErrInvalid
	}
	if err := validateMappingPeriods(tenant, c.Periods); err != nil {
		return err
	}
	if c.PriceEstimates != nil {
		if err := ValidatePriceEstimates(*c.PriceEstimates); err != nil {
			return err
		}
	}
	if c.HistoricalPeriods != nil {
		return validateMappingPeriods(tenant, *c.HistoricalPeriods)
	}
	return nil
}

func ValidatePriceEstimates(estimates []PriceEstimate) error {
	if len(estimates) > 100 {
		return ErrInvalid
	}
	for i, e := range estimates {
		if !decimalPattern.MatchString(e.Price) {
			return ErrInvalid
		}
		price, ok := new(big.Rat).SetString(e.Price)
		if !ok || len(e.Price) > 100 || price.Sign() < 0 || (e.Currency != "RUB" && e.Currency != "USD" && e.Currency != "EUR") || !mappingText(e.Reference, 2000) || e.At.Year() < 1900 || e.At.Year() > 2200 || e.From.Year() > 2200 || e.From.Before(e.At) || e.At.Nanosecond()%1000 != 0 || e.From.Nanosecond()%1000 != 0 || i > 0 && !e.From.After(estimates[i-1].From) {
			return ErrInvalid
		}
	}
	return nil
}

func validateMappingPeriods(tenant string, periods []MappingPeriod) error {
	if len(periods) > 100 {
		return ErrInvalid
	}
	for i, p := range periods {
		if p.NativeInstrument != nil && (ValidateInstrument(*p.NativeInstrument) != nil || !p.NativeInstrument.Detailed || p.NativeInstrument.ID != p.InstrumentID || p.NativeInstrument.Kind != p.Kind || p.NativeInstrument.Currency != p.Currency) {
			return ErrInvalid
		}
		if ValidateScope(Scope{tenant, p.Provider, p.AccessScope}) != nil || !mappingText(p.Evidence, 2000) {
			return ErrInvalid
		}
		id, e := uuid.Parse(p.InstrumentID)
		if e != nil || id.String() != p.InstrumentID {
			return ErrInvalid
		}
		if p.From.Year() < 1900 || p.From.Year() > 2200 || p.From.Nanosecond()%1000 != 0 || (!p.To.IsZero() && (p.To.Year() > 2200 || p.To.Nanosecond()%1000 != 0 || !p.To.After(p.From))) {
			return ErrInvalid
		}
		if i > 0 && (periods[i-1].To.IsZero() || p.From.Before(periods[i-1].To)) {
			return ErrInvalid
		}
		terms := p.Terms("validate", 1)
		quote := &calc.PriceFact{InstrumentID: p.InstrumentID, SourceKey: terms.SourceKey, Price: "1", Unit: p.Unit, Currency: p.Currency, Adjustment: "unadjusted", EffectiveAt: p.From, AvailableAt: p.From, Complete: true}
		// Cross-currency nominal terms are valid evidence but remain unusable for NAV
		// until their contractual conversion has been resolved.
		if _, e := calc.NormalizePrice(quote, &terms, nil, p.From); e != nil {
			return fmt.Errorf("%w: price convention", ErrInvalid)
		}
	}
	return nil
}

// HistoryPeriods separates raw historical listings from live quote providers.
// Old mappings retain their existing behavior, including explicit unknown
// adjustment gaps. A configured historical schedule never falls back through
// its gaps to an unrelated current listing.
func (m Mapping) HistoryPeriods() []MappingPeriod {
	if len(m.HistoricalPeriods) > 0 {
		return m.HistoricalPeriods
	}
	return m.Periods
}

func (p MappingPeriod) Terms(id string, revision int64) calc.PriceTerms {
	return calc.PriceTerms{ID: id, Revision: revision, InstrumentID: p.InstrumentID, SourceKey: p.Provider + "/" + p.AccessScope, Kind: p.Kind, Currency: p.Currency, Unit: p.Unit, Nominal: p.Nominal, NominalCurrency: p.NominalCurrency, LotSize: p.LotSize, From: p.From, To: p.To, Confirmed: true}
}

// TermsAt uses the mapping revision as provenance. It never bridges a gap.
func (m Mapping) TermsAt(at time.Time) (calc.PriceTerms, bool) {
	return m.termsAt(m.Periods, at, "")
}

func (m Mapping) HistoricalTermsAt(at time.Time) (calc.PriceTerms, bool) {
	return m.termsAt(m.HistoryPeriods(), at, "history:")
}

func (m Mapping) termsAt(periods []MappingPeriod, at time.Time, prefix string) (calc.PriceTerms, bool) {
	for i, p := range periods {
		if !at.Before(p.From) && (p.To.IsZero() || at.Before(p.To)) {
			return p.Terms(fmt.Sprintf("%s%s:%d", prefix, m.InstrumentKey, i), m.Version), true
		}
	}
	return calc.PriceTerms{}, false
}
