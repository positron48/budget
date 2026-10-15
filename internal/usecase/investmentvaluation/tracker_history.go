package investmentvaluation

import (
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"math/big"
	"slices"
	"time"
)

type TrackerHistoryRequest struct {
	LedgerIDs                       []string
	Currency, FXPolicyKey, From, To string
	PerformanceFrom                 string
}
type TrackerHistoryPoint struct {
	Date, State, CoverageKey   string
	KnownValue                 *big.Rat // Exact reporting-currency subtotal.
	Issues                     []calc.QualityIssue
	PricedPositions, Positions int
	StrategyFlow               *big.Rat
	FlowReason                 string
	Reset                      bool
}
type TrackerHistory struct {
	Currency, FXPolicyKey, FXMode, NextDate, BasisKey string
	FXPolicyVersion, Generation                       int64
	ReadAt                                            time.Time
	Points                                            []TrackerHistoryPoint
	Baseline                                          *TrackerHistoryPoint
}

// Bound total navigation to ten years, and each DB snapshot to 31 days. History
// always consists of completed Moscow dates, never a fabricated close for today.
func NormalizeTrackerHistory(q TrackerHistoryRequest, now time.Time) (TrackerHistoryRequest, error) {
	if len(q.LedgerIDs) < 1 || len(q.LedgerIDs) > 20 || q.FXPolicyKey == "" || len(q.FXPolicyKey) > 100 || (q.Currency != "RUB" && q.Currency != "USD" && q.Currency != "EUR") {
		return q, d.ErrInvestmentInvalid
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
