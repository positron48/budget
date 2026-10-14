package tinvest

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

// QuoteIdentifier contains public listing identifiers, never holdings or cash.
// Key is the application's exact instrument key and is not sent to the provider.
type QuoteIdentifier struct {
	Key      string `json:"key"`
	Ticker   string `json:"ticker"`
	Exchange string `json:"exchange"`
	ISIN     string `json:"isin,omitempty"`
	UID      string `json:"uid,omitempty"`
}

func ValidateQuoteIdentifier(q QuoteIdentifier) error {
	if q.Key == "" || len(q.Key) > 200 || strings.TrimSpace(q.Key) != q.Key || q.Ticker == "" || len(q.Ticker) > 64 || strings.TrimSpace(q.Ticker) != q.Ticker {
		return errors.New("invalid quote identifier")
	}
	if q.UID != "" {
		id, e := uuid.Parse(q.UID)
		if e != nil || id.String() != q.UID {
			return errors.New("invalid quote UID")
		}
	}
	if q.ISIN != "" && len(q.ISIN) != 12 {
		return errors.New("invalid quote ISIN")
	}
	switch q.Exchange {
	case "MCX", "NYSE", "NASDAQ", "AMEX", "OTC", "SN", "LSE":
	default:
		return errors.New("explicit supported listing exchange required")
	}
	return nil
}
func SelectQuoteInstrument(q QuoteIdentifier, candidates []Instrument) (Instrument, error) {
	if e := ValidateQuoteIdentifier(q); e != nil {
		return Instrument{}, e
	}
	if (q.Exchange == "SN" || q.Exchange == "LSE") && q.ISIN == "" {
		return Instrument{}, errors.New("cross-venue price requires confirmed ISIN")
	}
	var matches []Instrument
	for _, item := range candidates {
		if item.Ticker != q.Ticker || q.ISIN != "" && item.ISIN != q.ISIN || q.UID != "" && item.UID != q.UID {
			continue
		}
		if q.Exchange == "MCX" {
			if !strings.HasPrefix(item.ClassCode, "TQ") {
				continue
			}
		} else if strings.HasPrefix(item.ClassCode, "TQ") || q.UID == "" && item.ClassCode != "SPBXM" {
			continue
		}
		if item.InstrumentType != "share" && item.InstrumentType != "bond" && item.InstrumentType != "etf" {
			continue
		}
		matches = append(matches, item)
	}
	if len(matches) != 1 {
		return Instrument{}, errors.New("listing identity is missing or ambiguous")
	}
	return matches[0], nil
}
