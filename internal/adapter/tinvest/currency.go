package tinvest

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CurrencyDetails is current metadata, not proof of historical conventions.
// Nominal currency and settlement currency are deliberately kept separate.
type CurrencyDetails struct {
	Instrument
	Currency    string `json:"currency"`
	ISOCurrency string `json:"isoCurrencyName"`
	Lot         int32  `json:"lot"`
	Nominal     struct {
		Quotation
		Currency string `json:"currency"`
	} `json:"nominal"`
	Exchange     string    `json:"exchange"`
	RealExchange string    `json:"realExchange"`
	FirstDaily   time.Time `json:"first1dayCandleDate"`
}

func (c *Client) CurrencyByUID(ctx context.Context, id string) (CurrencyDetails, error) {
	var result struct {
		Instrument CurrencyDetails `json:"instrument"`
	}
	if parsed, e := uuid.Parse(id); e != nil || parsed.String() != id {
		return result.Instrument, errors.New("invalid currency UID")
	}
	if e := c.read(ctx, "InstrumentsService/CurrencyBy", map[string]any{"idType": "INSTRUMENT_ID_TYPE_UID", "id": id}, &result); e != nil {
		return CurrencyDetails{}, e
	}
	v := result.Instrument
	return normalizeCurrency(v, id)
}

func normalizeCurrency(v CurrencyDetails, id string) (CurrencyDetails, error) {
	v.Currency = strings.ToUpper(v.Currency)
	v.ISOCurrency = strings.ToUpper(v.ISOCurrency)
	v.Nominal.Currency = strings.ToUpper(v.Nominal.Currency)
	for _, currency := range []string{v.Currency, v.ISOCurrency, v.Nominal.Currency} {
		if len(currency) != 3 || strings.Trim(currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
			return CurrencyDetails{}, errors.New("invalid currency metadata")
		}
	}
	nominal, e := v.Nominal.Decimal()
	n, ok := new(big.Rat).SetString(nominal)
	if e != nil || !ok || n.Sign() <= 0 || v.UID != id || v.Lot <= 0 || v.Ticker == "" || v.ClassCode == "" {
		return CurrencyDetails{}, errors.New("invalid currency metadata")
	}
	return v, nil
}

// CurrencyCatalogEntry retains unusable metadata as audit evidence, not FX facts.
type CurrencyCatalogEntry struct {
	Metadata        CurrencyDetails `json:"metadata"`
	ValidationError string          `json:"validation_error,omitempty"`
}

// AllCurrencies includes archived listings omitted by FindInstrument. This is
// a small, explicitly requested currency catalog, not a full security dump.
func (c *Client) AllCurrencies(ctx context.Context) ([]CurrencyCatalogEntry, error) {
	var result struct {
		Instruments []CurrencyDetails `json:"instruments"`
	}
	if err := c.read(ctx, "InstrumentsService/Currencies", map[string]string{"instrumentStatus": "INSTRUMENT_STATUS_ALL"}, &result); err != nil {
		return nil, err
	}
	if len(result.Instruments) > 1000 {
		return nil, errors.New("currency catalog exceeds limit")
	}
	seen := map[string]bool{}
	entries := make([]CurrencyCatalogEntry, 0, len(result.Instruments))
	for _, v := range result.Instruments {
		if parsed, err := uuid.Parse(v.UID); err != nil || parsed.String() != v.UID || seen[v.UID] {
			return nil, errors.New("invalid currency catalog identity")
		}
		seen[v.UID] = true
		// Archived or virtual records can have unusable terms. Retain them as
		// catalog evidence with a safe validation code, never as FX facts.
		v.Currency = strings.ToUpper(v.Currency)
		v.ISOCurrency = strings.ToUpper(v.ISOCurrency)
		v.Nominal.Currency = strings.ToUpper(v.Nominal.Currency)
		normalized, err := normalizeCurrency(v, v.UID)
		if err != nil {
			entries = append(entries, CurrencyCatalogEntry{Metadata: v, ValidationError: err.Error()})
		} else {
			entries = append(entries, CurrencyCatalogEntry{Metadata: normalized})
		}
	}
	return entries, nil
}
