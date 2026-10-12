package investmentvaluation

import (
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"time"
)

// CurrentPolicy never borrows a daily close as a live quote. A daily convention
// requires an explicit operator-confirmed last-price SLA for the same feed.
func (p SeriesPolicy) CurrentPolicy() (SeriesPolicy, bool) {
	if p.Kind == "daily_candle" && p.CurrentMaxQuoteAge > 0 && p.CurrentMaxQuoteAge <= 30*24*time.Hour {
		return SeriesPolicy{Kind: "last_price", SourceKind: p.SourceKind, Adjustment: p.Adjustment, MaxQuoteAge: p.CurrentMaxQuoteAge}, true
	}
	if p.Kind == "last_price" && p.Retrospective == nil && p.MaxQuoteAge > 0 && p.MaxQuoteAge <= 30*24*time.Hour {
		return p, true
	}
	return SeriesPolicy{}, false
}

type CurrentQuote struct {
	InstrumentKey          string
	Scope                  md.Scope
	SourceKind, Adjustment string
	MaxAge                 time.Duration
	Price                  md.SelectedPrice
}
type CurrentEvidence struct {
	Quotes    []CurrentQuote
	FXFixings []calc.FXFixing
}
type CurrentNAV struct {
	SeriesKey, LastCompletedDate string
	Result                       Result
	Quotes                       []CurrentQuote
	FXFixings                    []calc.FXFixing
}
