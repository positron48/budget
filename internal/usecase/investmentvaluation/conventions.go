package investmentvaluation

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	d "github.com/positron48/budget/internal/domain"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// PriceConventionPeriod binds reviewed price semantics to one listing and
// source. A ticker match or a provider's current metadata is not confirmation.
type PriceConventionPeriod struct {
	md.Scope
	InstrumentID string
	From, To     time.Time // half-open; zero To means open-ended
	Policy       SeriesPolicy
}
type ConventionProfile struct {
	Key, Title, Evidence string
	Enabled              bool
	Periods              map[string][]PriceConventionPeriod   // journal instrument key
	Adjustments          map[string][]ReviewedPriceAdjustment `json:",omitempty"`
	Accrued              map[string][]ReviewedAccruedPeriod   `json:",omitempty"`
}
type SavedConventionProfile struct {
	ConventionProfile
	Version    int64
	ActorID    string
	RecordedAt time.Time
}

var conventionKey = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func ValidConventionKey(key string) bool { return conventionKey.MatchString(key) }
func conventionText(s string, max int) bool {
	return len(s) > 0 && len(s) <= max && strings.TrimSpace(s) == s && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func conventionTime(at time.Time) bool {
	return !at.IsZero() && at.Year() >= 1900 && at.Year() <= 2200 && at.Nanosecond()%1000 == 0
}
func ValidateConventionProfile(tenant string, p ConventionProfile) error {
	if !ValidConventionKey(p.Key) || !conventionText(p.Title, 200) || !conventionText(p.Evidence, 2000) || len(p.Periods) > 500 {
		return d.ErrInvestmentInvalid
	}
	total, sessions := 0, 0
	for key, periods := range p.Periods {
		if !conventionText(key, 200) || len(periods) == 0 || len(periods) > 100 {
			return d.ErrInvestmentInvalid
		}
		total += len(periods)
		if total > 1000 {
			return d.ErrInvestmentInvalid
		}
		for i, v := range periods {
			if v.TenantID != tenant || md.ValidateScope(v.Scope) != nil || !conventionTime(v.From) || (!v.To.IsZero() && (!conventionTime(v.To) || !v.To.After(v.From))) || i > 0 && (periods[i-1].To.IsZero() || v.From.Before(periods[i-1].To)) {
				return d.ErrInvestmentInvalid
			}
			if v.Policy.CurrentMaxQuoteAge < 0 || v.Policy.CurrentMaxQuoteAge > 30*24*time.Hour || (v.Policy.Kind != "daily_candle" && v.Policy.CurrentMaxQuoteAge != 0) {
				return d.ErrInvestmentInvalid
			}
			q := md.PriceSelectionRequest{Scope: v.Scope, InstrumentID: v.InstrumentID, Kind: v.Policy.Kind, SourceKind: v.Policy.SourceKind, Adjustment: v.Policy.Adjustment, At: v.From, Calendar: v.Policy.Calendar, MaxQuoteAge: v.Policy.MaxQuoteAge, Retrospective: v.Policy.Retrospective}
			if v.Policy.Kind == "daily_candle" && v.Policy.Retrospective == nil {
				c := v.Policy.Calendar
				if c == nil || !conventionTime(c.From) || !conventionTime(c.Through) || v.Policy.MaxQuoteAge != 0 {
					return d.ErrInvestmentInvalid
				}
				sessions += len(c.Sessions)
				if sessions > 20000 {
					return d.ErrInvestmentInvalid
				}
				for _, s := range c.Sessions {
					if !conventionTime(s.ObservationAt) || !conventionTime(s.CloseAt) || !conventionTime(s.AvailableAt) {
						return d.ErrInvestmentInvalid
					}
				}
				q.At = c.From // Validate the entire calendar, even outside period coverage.
			} else if v.Policy.Calendar != nil {
				return d.ErrInvestmentInvalid
			}
			if _, e := md.SelectValuationPrice(q, nil); e != nil {
				return d.ErrInvestmentInvalid
			}
		}
	}
	if err := validateAdjustmentPeriods(tenant, p); err != nil {
		return err
	}
	return validateAccruedPeriods(tenant, p)
}
func (p ConventionProfile) At(key string, at time.Time) (PriceConventionPeriod, bool) {
	if !p.Enabled {
		return PriceConventionPeriod{}, false
	}
	for _, v := range p.Periods[key] {
		if !at.Before(v.From) && (v.To.IsZero() || at.Before(v.To)) {
			return v, true
		}
	}
	return PriceConventionPeriod{}, false
}
