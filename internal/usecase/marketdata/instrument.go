package marketdata

import (
	"github.com/google/uuid"
	"strings"
	"time"
	"unicode/utf8"
)

// Instrument is current provider metadata, not historical terms or a confirmed
// link to an imported security. Same ticker/ISIN may have multiple listings.
type Instrument struct {
	Detailed  bool   `json:"detailed"`
	ID        string `json:"id"`
	FIGI      string `json:"figi"`
	ISIN      string `json:"isin"`
	Ticker    string `json:"ticker"`
	ClassCode string `json:"class_code"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Currency  string `json:"currency"`
}
type SavedInstrument struct {
	Instrument
	Revision   int64
	ReceivedAt time.Time
}

func ValidateInstrument(i Instrument) error {
	if id, err := uuid.Parse(i.ID); err != nil || id.String() != i.ID {
		return ErrInvalid
	}
	for _, s := range []string{i.FIGI, i.ISIN, i.Ticker, i.ClassCode, i.Name, i.Kind, i.Currency} {
		if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			return ErrInvalid
		}
	}
	if strings.TrimSpace(i.Ticker) == "" || len(i.Ticker) > 100 || len(i.ClassCode) > 100 || strings.TrimSpace(i.Name) == "" || len(i.Name) > 1000 || len(i.FIGI) > 64 || len(i.ISIN) > 32 {
		return ErrInvalid
	}
	switch i.Kind {
	case "share", "bond", "etf", "currency", "futures", "option", "sp", "index", "commodity", "clearing_certificate", "dfa":
	default:
		return ErrInvalid
	}
	if i.Currency != "" && (len(i.Currency) != 3 || strings.Trim(i.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "") {
		return ErrInvalid
	}
	return nil
}

type CatalogResult struct {
	Instruments []Instrument
	Truncated   bool
}
