// Package marketdata stores provider evidence separately from confirmed
// financial instrument mappings and normalized valuation prices.
package marketdata

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrInvalid = errors.New("invalid market observation")

// Scope is an entitlement boundary, not a credential. No tokens belong here.
// Until redistribution rights are established, all observations are tenant scoped.
type Scope struct {
	TenantID    string
	Provider    string
	AccessScope string
}

// Observation retains provider units and timestamp semantics. In particular,
// a daily candle timestamp is its start, not the time its close became known.
// Currency/unit/adjustment may be unknown; that prevents valuation, not storage.
type Observation struct {
	InstrumentID string    `json:"instrument_id"`
	Kind         string    `json:"kind"`
	At           time.Time `json:"at"`
	SourceKind   string    `json:"source_kind"`
	Currency     string    `json:"currency"`
	PriceUnit    string    `json:"price_unit"`
	Adjustment   string    `json:"adjustment"`
	Price        string    `json:"price"`
	Open         string    `json:"open,omitempty"`
	High         string    `json:"high,omitempty"`
	Low          string    `json:"low,omitempty"`
	Volume       string    `json:"volume,omitempty"`
	Complete     bool      `json:"complete"`
}

type SavedObservation struct {
	Observation
	ID         string
	Revision   int64
	ReceivedAt time.Time
}

var scopePattern = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`)
var decimalPattern = regexp.MustCompile(`^-?[0-9]{1,24}(\.[0-9]{1,9})?$`)
var volumePattern = regexp.MustCompile(`^[0-9]{1,19}$`)

func ValidateScope(s Scope) error {
	if id, err := uuid.Parse(s.TenantID); err != nil || id.String() != s.TenantID {
		return ErrInvalid
	}
	if s.Provider != "tinvest" || !scopePattern.MatchString(s.AccessScope) {
		return ErrInvalid
	}
	return nil
}
func decimal(value string) (string, *big.Rat, error) {
	if !decimalPattern.MatchString(value) {
		return "", nil, ErrInvalid
	}
	n, ok := new(big.Rat).SetString(value)
	if !ok {
		return "", nil, ErrInvalid
	}
	return n.FloatString(9), n, nil
}

// Normalize makes economically identical decimal inputs idempotent. Unknown
// units remain explicit; no bond-point, FX nominal or split adjustment is guessed.
func Normalize(o Observation) (Observation, error) {
	if id, err := uuid.Parse(o.InstrumentID); err != nil || id.String() != o.InstrumentID {
		return o, ErrInvalid
	}
	if o.Kind != "last_price" && o.Kind != "daily_candle" {
		return o, ErrInvalid
	}
	if o.SourceKind != "exchange" && o.SourceKind != "dealer" {
		return o, ErrInvalid
	}
	if o.At.IsZero() || o.At.Year() < 1900 || o.At.Year() > 2200 || o.At.Nanosecond()%1000 != 0 {
		return o, ErrInvalid
	}
	o.At = o.At.UTC()
	if o.Currency != "" && (len(o.Currency) != 3 || strings.Trim(o.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "") {
		return o, ErrInvalid
	}
	if o.PriceUnit != "provider_unspecified" && o.PriceUnit != "currency_per_unit" && o.PriceUnit != "percent_of_nominal" {
		return o, ErrInvalid
	}
	if o.Adjustment != "provider_unspecified" && o.Adjustment != "unadjusted" {
		return o, ErrInvalid
	}
	var err error
	var price *big.Rat
	o.Price, price, err = decimal(o.Price)
	if err != nil {
		return o, err
	}
	if o.Kind == "last_price" {
		if o.Open != "" || o.High != "" || o.Low != "" || o.Volume != "" || !o.Complete {
			return o, ErrInvalid
		}
		return o, nil
	}
	var open, high, low *big.Rat
	o.Open, open, err = decimal(o.Open)
	if err != nil {
		return o, err
	}
	o.High, high, err = decimal(o.High)
	if err != nil {
		return o, err
	}
	o.Low, low, err = decimal(o.Low)
	if err != nil {
		return o, err
	}
	if low.Cmp(high) > 0 || open.Cmp(low) < 0 || open.Cmp(high) > 0 || price.Cmp(low) < 0 || price.Cmp(high) > 0 {
		return o, ErrInvalid
	}
	if !volumePattern.MatchString(o.Volume) {
		return o, ErrInvalid
	}
	volume, ok := new(big.Int).SetString(o.Volume, 10)
	if !ok || !volume.IsInt64() {
		return o, ErrInvalid
	}
	o.Volume = volume.String()
	return o, nil
}
