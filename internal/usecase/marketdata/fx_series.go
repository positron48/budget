package marketdata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"math/big"
	"time"
)

// FXSeries is an operator-reviewed binding, never inferred from a currency
// ticker or today's nominal. All intervals refer to provider candle starts.
// Offsets describe reviewed publication semantics; validity is not guessed.
type FXSeries struct {
	InstrumentID, FromCurrency, ToCurrency                       string
	From, To                                                     time.Time
	RateKind, SourceKind, PriceUnit, Adjustment                  string
	Multiplier, Divisor                                          string
	EffectiveOffsetSeconds, AvailableOffsetSeconds, ValidSeconds int64
	RequirePositiveVolume                                        bool
	SourceReference, Evidence                                    string
}

func ValidateFXSeries(p FXPolicy) error {
	if len(p.Series) > 100 {
		return ErrLimit
	}
	if len(p.Series) > 0 && (p.Mode != "market" || p.Source != "tinvest") {
		return ErrInvalid
	}
	for i, s := range p.Series {
		u, e := uuid.Parse(s.InstrumentID)
		if e != nil || u.String() != s.InstrumentID {
			return ErrInvalid
		}
		if (s.FromCurrency != "RUB" && s.FromCurrency != "USD" && s.FromCurrency != "EUR") || (s.ToCurrency != "RUB" && s.ToCurrency != "USD" && s.ToCurrency != "EUR") || s.FromCurrency == s.ToCurrency {
			return ErrInvalid
		}
		for _, t := range []time.Time{s.From, s.To} {
			if !t.IsZero() && (t.Year() < 1900 || t.Year() > 2200 || !t.Equal(t.UTC().Truncate(24*time.Hour))) {
				return ErrInvalid
			}
		}
		if s.From.IsZero() || !s.To.IsZero() && !s.To.After(s.From) || (s.RateKind != "exchange_spot" && s.RateKind != "otc_indicative") || (s.SourceKind != "exchange" && s.SourceKind != "dealer") || (s.PriceUnit != "provider_unspecified" && s.PriceUnit != "currency_per_unit") || (s.Adjustment != "unadjusted" && s.Adjustment != "provider_unspecified") || s.EffectiveOffsetSeconds < 0 || s.EffectiveOffsetSeconds > 172800 || s.AvailableOffsetSeconds < s.EffectiveOffsetSeconds || s.AvailableOffsetSeconds > 172800 || s.ValidSeconds < 1 || s.ValidSeconds > 14*86400 || !mappingText(s.SourceReference, 2000) || !mappingText(s.Evidence, 4000) {
			return ErrInvalid
		}
		for _, v := range []string{s.Multiplier, s.Divisor} {
			if !fxDecimal.MatchString(v) {
				return ErrInvalid
			}
			n, _ := new(big.Rat).SetString(v)
			if n.Sign() <= 0 {
				return ErrInvalid
			}
		}
		for _, old := range p.Series[:i] {
			samePair := (old.FromCurrency == s.FromCurrency && old.ToCurrency == s.ToCurrency) || (old.FromCurrency == s.ToCurrency && old.ToCurrency == s.FromCurrency)
			overlap := (old.To.IsZero() || s.From.Before(old.To)) && (s.To.IsZero() || old.From.Before(s.To))
			if (samePair || old.InstrumentID == s.InstrumentID) && overlap {
				return ErrInvalid
			}
			// Different publication conventions must not collapse adjacent raw
			// periods onto the same effective fixing interval.
			oldStart := old.From.Add(time.Duration(old.EffectiveOffsetSeconds) * time.Second)
			newStart := s.From.Add(time.Duration(s.EffectiveOffsetSeconds) * time.Second)
			oldEnd := old.To.Add(time.Duration(old.EffectiveOffsetSeconds) * time.Second)
			newEnd := s.To.Add(time.Duration(s.EffectiveOffsetSeconds) * time.Second)
			if samePair && (old.To.IsZero() || newStart.Before(oldEnd)) && (s.To.IsZero() || oldStart.Before(newEnd)) {
				return ErrInvalid
			}
		}
	}
	return nil
}

// FXSeriesPoint returns a withdrawal marker for an observation within the
// approved binding whose price/unit/completeness is no longer trustworthy.
// That revision prevents resurrection of an earlier fixing with longer life.
func FXSeriesPoint(s FXSeries, o SavedObservation) (FXPoint, bool, error) {
	if o.InstrumentID != s.InstrumentID || o.Kind != "daily_candle" || o.SourceKind != s.SourceKind || o.Adjustment != s.Adjustment || o.At.Before(s.From) || !s.To.IsZero() && !o.At.Before(s.To) {
		return FXPoint{}, false, nil
	}
	if o.ID == "" || o.Revision < 1 || o.ReceivedAt.IsZero() {
		return FXPoint{}, false, ErrInvalid
	}
	normalized, e := Normalize(o.Observation)
	if e != nil {
		return FXPoint{}, false, e
	}
	raw, e := json.Marshal(normalized)
	if e != nil {
		return FXPoint{}, false, e
	}
	sum := sha256.Sum256(raw)
	p := FXPoint{From: s.FromCurrency, To: s.ToCurrency, At: o.At.Add(time.Duration(s.EffectiveOffsetSeconds) * time.Second), AvailableAt: o.At.Add(time.Duration(s.AvailableOffsetSeconds) * time.Second), EvidenceSHA256: hex.EncodeToString(sum[:]), RecordReference: fmt.Sprintf("market_observation/%s@%d", o.ID, o.Revision)}
	p.ValidUntil = p.AvailableAt.Add(time.Duration(s.ValidSeconds) * time.Second)
	price, _ := new(big.Rat).SetString(normalized.Price)
	p.Withdrawn = !o.Complete || o.SourceKind != s.SourceKind || o.Currency != s.ToCurrency || o.PriceUnit != s.PriceUnit || o.Adjustment != s.Adjustment || price.Sign() <= 0 || (s.RequirePositiveVolume && o.Volume == "0") || p.AvailableAt.After(o.ReceivedAt)
	if !p.Withdrawn {
		m, _ := new(big.Rat).SetString(s.Multiplier)
		d, _ := new(big.Rat).SetString(s.Divisor)
		price.Mul(price, m)
		price.Quo(price, d)
		// FX storage permits finite decimals up to 18 digits. Never silently round a
		// non-terminating nominal conversion into a seemingly exact market rate.
		text := price.FloatString(18)
		check, _ := new(big.Rat).SetString(text)
		if check.Cmp(price) != 0 || !fxDecimal.MatchString(text) {
			p.Withdrawn = true
		} else {
			p.Rate = text
		}
	}
	p, e = NormalizeFXPoint(p)
	return p, true, e
}
