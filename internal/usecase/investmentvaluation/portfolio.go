package investmentvaluation

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
)

type PortfolioInput struct {
	TrackerSelection                               bool `json:",omitempty"`
	AutoExtend                                     bool `json:",omitempty"`
	Archived                                       bool `json:",omitempty"`
	ID, Name                                       string
	LedgerIDs                                      []string
	Currency, FXPolicyKey, ConventionKey, From, To string
	ExpectedVersion                                int64
	RequestKey                                     string
}
type Portfolio struct {
	TrackerSelection                                          bool `json:",omitempty"`
	AutoExtend                                                bool `json:",omitempty"`
	Archived                                                  bool `json:",omitempty"`
	ID, Name                                                  string
	LedgerIDs                                                 []string
	Currency, FXPolicyKey, ConventionKey, From, To, SeriesKey string
	Version                                                   int64
	UpdatedAt                                                 time.Time
}
type ProfileOption struct {
	Key, Title, Mode string
	Version          int64
}
type PortfolioOptions struct {
	Price []ProfileOption
	FX    []ProfileOption
}

func NormalizePortfolio(p PortfolioInput) (PortfolioInput, error) {
	p.Name = strings.TrimSpace(p.Name)
	if !conventionText(p.Name, 200) || !conventionText(p.RequestKey, 200) || p.ExpectedVersion < 0 || len(p.LedgerIDs) < 1 || len(p.LedgerIDs) > 20 || p.ExpectedVersion == 1<<63-1 {
		return p, d.ErrInvestmentInvalid
	}
	if p.ID != "" {
		if id, e := uuid.Parse(p.ID); e != nil || id.String() != p.ID || p.ExpectedVersion == 0 {
			return p, d.ErrInvestmentInvalid
		}
	} else if p.ExpectedVersion != 0 || p.Archived {
		return p, d.ErrInvestmentInvalid
	}
	p.LedgerIDs = append([]string{}, p.LedgerIDs...)
	sort.Strings(p.LedgerIDs)
	for i, id := range p.LedgerIDs {
		if parsed, e := uuid.Parse(id); e != nil || parsed.String() != id || i > 0 && p.LedgerIDs[i-1] == id {
			return p, d.ErrInvestmentInvalid
		}
	}
	if p.TrackerSelection {
		if p.AutoExtend || p.Currency != "" || p.FXPolicyKey != "" || p.ConventionKey != "" || p.From != "" || p.To != "" {
			return p, d.ErrInvestmentInvalid
		}
		return p, nil
	}
	if !ValidConventionKey(p.ConventionKey) || !conventionText(p.FXPolicyKey, 100) || (p.Currency != "RUB" && p.Currency != "USD" && p.Currency != "EUR") {
		return p, d.ErrInvestmentInvalid
	}
	from, e := DayCutoff(p.From)
	if e != nil {
		return p, d.ErrInvestmentInvalid
	}
	to, e := DayCutoff(p.To)
	if e != nil || to.Before(from) || to.After(time.Now()) {
		return p, d.ErrInvestmentInvalid
	}
	// Calendar-day bounds do not depend on historical Moscow DST changes.
	first, _ := time.Parse(time.DateOnly, p.From)
	last, _ := time.Parse(time.DateOnly, p.To)
	if last.Sub(first) > 3660*24*time.Hour {
		return p, d.ErrInvestmentInvalid
	}
	return p, nil
}

// LastCompletedDate is based on the reporting calendar, not 24 elapsed hours.
func LastCompletedDate(now time.Time) string {
	loc, _ := time.LoadLocation("Europe/Moscow")
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1).Format(time.DateOnly)
}
