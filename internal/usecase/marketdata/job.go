package marketdata

import (
	"errors"
	"github.com/google/uuid"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrLeaseLost = errors.New("market job lease lost")
var ErrNoJob = errors.New("no market job available")

type JobRequest struct {
	SearchQuery    string
	InstrumentKind string
	InstrumentID   string
	Kind           string
	From           time.Time
	To             time.Time
}
type Job struct {
	JobRequest
	Scope      Scope
	ID         string
	LeaseToken string
	LeaseUntil time.Time
	Attempts   int
	Status     string
}

func NormalizeRequest(r JobRequest) JobRequest {
	r.SearchQuery = strings.ToLower(strings.TrimSpace(r.SearchQuery))
	return r
}
func ValidateRequest(r JobRequest) error {
	if r.Kind == "catalog_search" {
		if r.InstrumentID != "" || !r.From.IsZero() || !r.To.IsZero() || len(strings.TrimSpace(r.SearchQuery)) < 1 || len(r.SearchQuery) > 100 || !utf8.ValidString(r.SearchQuery) || strings.ContainsRune(r.SearchQuery, 0) {
			return ErrInvalid
		}
		switch r.InstrumentKind {
		case "share", "bond", "etf", "currency":
			return nil
		default:
			return ErrInvalid
		}
	}
	if r.SearchQuery != "" || r.InstrumentKind != "" {
		return ErrInvalid
	}
	if id, err := uuid.Parse(r.InstrumentID); err != nil || id.String() != r.InstrumentID {
		return ErrInvalid
	}
	if r.Kind == "last_price" {
		if !r.From.IsZero() || !r.To.IsZero() {
			return ErrInvalid
		}
		return nil
	}
	if (r.Kind != "daily_candle" && r.Kind != "bond_facts" && r.Kind != "dividend_facts") || r.From.Year() < 1900 || r.To.Year() > 2200 || !r.To.After(r.From) || r.To.Sub(r.From) > 365*24*time.Hour || r.From.Nanosecond() != 0 || r.To.Nanosecond() != 0 {
		return ErrInvalid
	}
	return nil
}
