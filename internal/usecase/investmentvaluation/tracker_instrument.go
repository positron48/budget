package investmentvaluation

import (
	"encoding/hex"
	"math/big"
	"strings"
	"time"

	d "github.com/positron48/budget/internal/domain"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type (
	TrackerInstrumentRequest struct {
		InstrumentKey, Currency, FXPolicyKey, From, To, ListingID, Provider, AccessScope, InstrumentID string
		PageDays                                                                                       int
	}
	TrackerInstrumentListing struct {
		Enabled    bool
		ID         string
		Period     md.MappingPeriod
		Instrument *md.Instrument
	}
)

type TrackerInstrumentPoint struct {
	Date, State, NativeCurrency, Unit, Source, ObservationID, CandleDate string
	Revision                                                             int64
	Raw, NativeClean, Clean, Accrued, NativeDirty, Dirty                 *big.Rat
	Nominal, NominalCurrency                                             string
	Issues                                                               []string
}
type TrackerInstrumentHistory struct {
	InstrumentKey, Currency, FXPolicyKey, FXMode, NextDate, BasisKey string
	FXPolicyVersion                                                  int64
	ReadAt                                                           time.Time
	Listings                                                         []TrackerInstrumentListing
	Instrument                                                       *md.Instrument
	MetadataAt                                                       time.Time
	Quote                                                            *PortfolioQuote
	Bond                                                             *md.SavedBondFacts
	Scope                                                            md.Scope
	MetadataInstrumentID                                             string
	MetadataEnabled                                                  bool
	BondLimited                                                      bool
	ListingOnly                                                      bool
	Points                                                           []TrackerInstrumentPoint
}

func NormalizeTrackerInstrument(q TrackerInstrumentRequest, now time.Time) (TrackerInstrumentRequest, error) {
	if q.PageDays < 0 || q.PageDays > 93 {
		return q, d.ErrInvestmentInvalid
	}
	if keys, e := NormalizeQuoteKeys([]string{q.InstrumentKey}); e != nil || len(keys) != 1 || q.InstrumentKey == "" {
		return q, d.ErrInvestmentInvalid
	}
	if len(q.FXPolicyKey) > 100 || q.Currency != "RUB" && q.Currency != "USD" && q.Currency != "EUR" || len(q.ListingID) > 64 {
		return q, d.ErrInvestmentInvalid
	}
	a, e := time.Parse(time.DateOnly, q.From)
	b, e2 := time.Parse(time.DateOnly, q.To)
	if e != nil || e2 != nil || q.From < "1900-01-01" || b.Before(a) || b.Sub(a) > 3660*24*time.Hour || q.To > LastCompletedDate(now) {
		return q, d.ErrInvestmentInvalid
	}
	if strings.ContainsAny(q.InstrumentKey, "\x00\r\n\t") {
		return q, d.ErrInvestmentInvalid
	}
	if q.ListingID != "" {
		b, e := hex.DecodeString(q.ListingID)
		if e != nil || len(b) != 32 || hex.EncodeToString(b) != q.ListingID {
			return q, d.ErrInvestmentInvalid
		}
	}
	if q.Provider != "" || q.AccessScope != "" || q.InstrumentID != "" {
		if md.ValidateScope(md.Scope{TenantID: "00000000-0000-0000-0000-000000000000", Provider: q.Provider, AccessScope: q.AccessScope}) != nil || md.ValidateRequest(md.JobRequest{Kind: "last_price", InstrumentID: q.InstrumentID}) != nil || q.InstrumentKey != q.Provider+":"+q.InstrumentID || strings.ContainsAny(q.AccessScope, "\x00\r\n") {
			return q, d.ErrInvestmentInvalid
		}
	}
	return q, nil
}
