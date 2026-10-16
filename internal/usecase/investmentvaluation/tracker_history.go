package investmentvaluation

import (
	"math/big"
	"slices"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

type TrackerHistoryRequest struct {
	CompactIssues                   bool
	PageDays                        int
	LedgerIDs                       []string
	Currency, FXPolicyKey, From, To string
	PerformanceFrom                 string
	DetailDate                      string // One-day breakdown within the original fenced chart range.
	GroupIDs                        []string
	InstrumentKeys                  []string
	MonetaryOnly                    bool
	ContributionOnly                bool // Internal boundary valuation; never a signed performance page.
}
type TrackerHistoryPoint struct {
	IssueCount                 int
	Date, State, CoverageKey   string
	KnownValue                 *big.Rat // Exact reporting-currency subtotal.
	Issues                     []calc.QualityIssue
	PricedPositions, Positions int
	StrategyFlow               *big.Rat
	FlowReason                 string
	Reset                      bool
	Components                 map[string]TrackerContributionValue
	LedgerReasons              map[string]string
	Details                    []TrackerValueDetail
	DetailFX                   map[string][]calc.FXLeg
	DetailFixings              []calc.FXFixing
}
type TrackerHistory struct {
	Currency, FXPolicyKey, FXMode, NextDate, BasisKey string
	FXPolicyVersion, Generation                       int64
	ClassificationVersion                             int64
	ReadAt                                            time.Time
	Points                                            []TrackerHistoryPoint
	Baseline                                          *TrackerHistoryPoint
	Contributions                                     []TrackerContribution
}

// Bound total navigation to ten years, and each DB snapshot to at most 93 days. History
// always consists of completed Moscow dates, never a fabricated close for today.
func NormalizeTrackerHistory(q TrackerHistoryRequest, now time.Time) (TrackerHistoryRequest, error) {
	if q.PageDays < 0 || q.PageDays > 93 {
		return q, d.ErrInvestmentInvalid
	}
	if len(q.LedgerIDs) < 1 || len(q.LedgerIDs) > 20 || q.FXPolicyKey == "" || len(q.FXPolicyKey) > 100 || (q.Currency != "RUB" && q.Currency != "USD" && q.Currency != "EUR") {
		return q, d.ErrInvestmentInvalid
	}
	if q.DetailDate != "" && (q.DetailDate < q.From || q.DetailDate > q.To || q.ContributionOnly || q.PerformanceFrom != "" || q.CompactIssues) {
		return q, d.ErrInvestmentInvalid
	}
	if q.DetailDate != "" {
		if day, e := time.Parse(time.DateOnly, q.DetailDate); e != nil || day.Format(time.DateOnly) != q.DetailDate {
			return q, d.ErrInvestmentInvalid
		}
	}
	if q.ContributionOnly && q.PerformanceFrom != "" {
		return q, d.ErrInvestmentInvalid
	}
	if len(q.GroupIDs) > 101 || len(q.GroupIDs) > 0 && q.PerformanceFrom != "" && !q.MonetaryOnly {
		return q, d.ErrInvestmentInvalid
	}
	var keyError error
	q.InstrumentKeys, keyError = NormalizeQuoteKeys(q.InstrumentKeys)
	if keyError != nil {
		return q, keyError
	}
	if q.MonetaryOnly && len(q.GroupIDs) == 0 {
		return q, d.ErrInvestmentInvalid
	}
	q.GroupIDs = slices.Clone(q.GroupIDs)
	slices.Sort(q.GroupIDs)
	q.GroupIDs = slices.Compact(q.GroupIDs)
	if len(q.GroupIDs) == 0 {
		q.GroupIDs = nil
	}
	for _, id := range q.GroupIDs {
		if id != "" {
			u, e := uuid.Parse(id)
			if e != nil || u.String() != id {
				return q, d.ErrInvestmentInvalid
			}
		}
	}
	q.LedgerIDs = slices.Clone(q.LedgerIDs)
	slices.Sort(q.LedgerIDs)
	for i, id := range q.LedgerIDs {
		u, e := uuid.Parse(id)
		if e != nil || u.String() != id || i > 0 && q.LedgerIDs[i-1] == id {
			return q, d.ErrInvestmentInvalid
		}
	}
	from, e := time.Parse(time.DateOnly, q.From)
	if e != nil || q.From < "1900-01-01" {
		return q, d.ErrInvestmentInvalid
	}
	to, e := time.Parse(time.DateOnly, q.To)
	if e != nil || to.Before(from) || to.Sub(from) > 3660*24*time.Hour || q.To > LastCompletedDate(now) {
		return q, d.ErrInvestmentInvalid
	}
	if q.PerformanceFrom != "" {
		start, e := time.Parse(time.DateOnly, q.PerformanceFrom)
		if e != nil || q.PerformanceFrom < "1900-01-01" || q.PerformanceFrom > q.From || to.Sub(start) > 3660*24*time.Hour {
			return q, d.ErrInvestmentInvalid
		}
	}
	return q, nil
}

// Values describe the exact clean valuation after recorded split rebasing.
type TrackerValueDetail struct {
	LedgerID, LedgerName, InstrumentKey, Currency, Source string
	Quantity, Price, NativeValue, Value, FXRate           *big.Rat
	PriceAt                                               time.Time
	MappingVersion                                        int64
	Issues                                                []string
}

// TrackerPageDays preserves the existing page contract for older clients.
func TrackerPageDays(days int) int {
	if days == 0 {
		return 31
	}
	return days
}
