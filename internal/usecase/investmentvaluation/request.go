package investmentvaluation

import (
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"time"
)

// SeriesPolicy is supplied by a trusted, reviewed convention registry, never
// inferred from a ticker or enabled by a public request's unchecked strings.
// Persistent convention storage is a separate adapter responsibility.
type SeriesPolicy struct {
	Kind, SourceKind, Adjustment string
	MaxQuoteAge                  time.Duration
	// Explicit opt-in SLA for a last-price provisional view of a daily series.
	CurrentMaxQuoteAge time.Duration `json:",omitempty"`
	Calendar           *md.SessionCalendar
	Retrospective      *md.RetrospectiveDailyPolicy `json:",omitempty"`
}
type Request struct {
	TenantID, UserID      string
	LedgerIDs             []string
	At                    time.Time
	Currency, FXPolicyKey string
	Series                map[string]SeriesPolicy // legacy internal convention injection; never public
	ConventionKey         string                  `json:",omitempty"` // persisted operator-confirmed profile, mutually exclusive with Series
}
