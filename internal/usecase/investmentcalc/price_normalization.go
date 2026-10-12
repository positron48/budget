package investmentcalc

import (
	"fmt"
	"math/big"
	"time"
)

// PriceTerms is a confirmed, effective-dated price convention. It must not be
// fabricated from current metadata for an earlier date. SourceKey identifies
// the provider/access scope expected by this convention, not a credential.
type PriceTerms struct {
	ID                       string
	Revision                 int64
	InstrumentID, SourceKey  string
	Kind, Currency, Unit     string
	Nominal, NominalCurrency string
	LotSize                  string    // descriptive only: position quantities are security/base units
	From, To                 time.Time // [From, To); absent To means no known end
	Confirmed                bool
}

// PriceFact separates economic time from availability. Candle interval-start
// timestamps alone are insufficient to populate these fields for an EOD close.
type PriceFact struct {
	InstrumentID, SourceKey           string
	Price, Currency, Unit, Adjustment string
	EffectiveAt, AvailableAt          time.Time
	Complete                          bool
}

// PriceAdjustmentProof records an operator-reviewed inversion of a provider
// adjustment. Raw observation and corporate evidence hashes remain independent.
type PriceAdjustmentProof struct {
	Basis, Nominal, NominalCurrency                                  string `json:",omitempty"`
	ProfileKey                                                       string
	ProfileVersion                                                   int64
	ObservationID                                                    string
	ObservationRevision                                              int64
	ObservationHash, SourcePrice, Multiplier, Divisor, ResolvedPrice string
	SourceReference, EvidenceSHA256, Reference                       string
}

// AccruedFact must come from an authoritative amount or verified coupon rules.
// An absent amount never means zero; no generic linear accrual is used here.
type AccruedFact struct {
	InstrumentID, SourceID, Currency, AmountPerUnit string
	From, To                                        time.Time // explicit validity interval; To is required
}

type NormalizedPrice struct {
	Currency              string
	Clean, Accrued, Dirty *big.Rat // nil denotes unknown, never zero
	TermsID               string
	TermsRevision         int64
	Issues                []string
}

func (p NormalizedPrice) Complete() bool { return p.Dirty != nil && len(p.Issues) == 0 }
func within(at, from, to time.Time) bool {
	return !from.IsZero() && !at.Before(from) && (to.IsZero() || at.Before(to))
}
func validMoneyCurrency(c string) bool {
	return len(c) == 3 && c[0] >= 'A' && c[0] <= 'Z' && c[1] >= 'A' && c[1] <= 'Z' && c[2] >= 'A' && c[2] <= 'Z'
}

// NormalizePrice resolves one native-currency unit price without rounding.
// Missing/conflicting evidence yields quality issues. Malformed inputs yield
// errors. Contractual cross-currency bond conversion must be resolved upstream;
// an analytical market/CBR fixing is not a substitute for issuer terms.
func NormalizePrice(quote *PriceFact, terms *PriceTerms, accrued *AccruedFact, at time.Time) (NormalizedPrice, error) {
	out := NormalizedPrice{Issues: []string{}}
	if at.IsZero() {
		return out, fmt.Errorf("valuation timestamp required")
	}
	if quote == nil {
		out.Issues = append(out.Issues, "quote_missing")
	}
	if terms == nil {
		out.Issues = append(out.Issues, "terms_missing")
	}
	if quote == nil || terms == nil {
		return out, nil
	}
	out.Currency = terms.Currency
	out.TermsID = terms.ID
	out.TermsRevision = terms.Revision
	if terms.ID == "" || terms.Revision < 1 || terms.InstrumentID == "" || terms.SourceKey == "" || !validMoneyCurrency(terms.Currency) || terms.From.IsZero() || (!terms.To.IsZero() && !terms.To.After(terms.From)) {
		return out, fmt.Errorf("invalid price terms")
	}
	if quote.InstrumentID != terms.InstrumentID || quote.SourceKey != terms.SourceKey {
		return out, fmt.Errorf("price identity or source mismatch")
	}
	if quote.EffectiveAt.IsZero() || quote.AvailableAt.IsZero() || quote.AvailableAt.Before(quote.EffectiveAt) {
		return out, fmt.Errorf("invalid quote timestamps")
	}
	if !terms.Confirmed {
		out.Issues = append(out.Issues, "terms_unconfirmed")
	}
	if !within(at, terms.From, terms.To) || !within(quote.EffectiveAt, terms.From, terms.To) {
		out.Issues = append(out.Issues, "terms_interval_mismatch")
	}
	if quote.EffectiveAt.After(at) || quote.AvailableAt.After(at) {
		out.Issues = append(out.Issues, "quote_not_yet_available")
	}
	if !quote.Complete {
		out.Issues = append(out.Issues, "quote_incomplete")
	}
	if quote.Adjustment != "unadjusted" {
		out.Issues = append(out.Issues, "unadjusted_price_required")
	}
	// A confirmed source convention can resolve an unspecified raw unit/currency.
	// Explicit contradictory provider metadata cannot be silently overridden.
	if quote.Currency != "" && quote.Currency != terms.Currency {
		out.Issues = append(out.Issues, "quote_currency_conflict")
	}
	if quote.Unit != "" && quote.Unit != "provider_unspecified" && quote.Unit != terms.Unit {
		out.Issues = append(out.Issues, "quote_unit_conflict")
	}
	price, err := number(quote.Price)
	if err != nil || price.Sign() < 0 {
		return out, fmt.Errorf("nonnegative raw price required")
	}
	if terms.LotSize != "" {
		lot, err := number(terms.LotSize)
		if err != nil || lot.Sign() <= 0 {
			return out, fmt.Errorf("positive lot size required")
		}
	}
	clean := copyRat(price)
	switch terms.Kind {
	case "share", "etf":
		if terms.Unit != "currency_per_unit" {
			return out, fmt.Errorf("invalid share/fund price convention")
		}
	case "bond":
		switch terms.Unit {
		case "percent_of_nominal":
			nominal, err := number(terms.Nominal)
			if err != nil || nominal.Sign() <= 0 {
				return out, fmt.Errorf("positive outstanding nominal required")
			}
			if !validMoneyCurrency(terms.NominalCurrency) {
				return out, fmt.Errorf("nominal currency required")
			}
			if terms.NominalCurrency != terms.Currency {
				out.Issues = append(out.Issues, "contractual_nominal_conversion_required")
			}
			clean.Quo(new(big.Rat).Mul(clean, nominal), big.NewRat(100, 1))
		case "currency_per_unit": // Explicit clean monetary quote; no nominal factor.
		default:
			return out, fmt.Errorf("invalid bond price convention")
		}
	case "currency":
		if terms.Unit != "currency_per_base_nominal" || !validMoneyCurrency(terms.NominalCurrency) || terms.NominalCurrency == terms.Currency {
			return out, fmt.Errorf("invalid currency quote convention")
		}
		nominal, err := number(terms.Nominal)
		if err != nil || nominal.Sign() <= 0 {
			return out, fmt.Errorf("positive base currency nominal required")
		}
		clean.Quo(clean, nominal)
	default:
		return out, fmt.Errorf("unsupported instrument kind")
	}
	if len(out.Issues) > 0 {
		return out, nil
	}
	out.Clean = clean
	if terms.Kind != "bond" {
		if accrued != nil {
			return NormalizedPrice{}, fmt.Errorf("accrued interest only applies to bonds")
		}
		out.Accrued = new(big.Rat)
		out.Dirty = copyRat(clean)
		return out, nil
	}
	if accrued == nil {
		out.Issues = append(out.Issues, "accrued_interest_missing")
		return out, nil
	}
	if accrued.InstrumentID != terms.InstrumentID || accrued.SourceID == "" || accrued.From.IsZero() || accrued.To.IsZero() || !accrued.To.After(accrued.From) {
		return NormalizedPrice{}, fmt.Errorf("invalid accrued interest evidence")
	}
	amount, err := number(accrued.AmountPerUnit)
	if err != nil || amount.Sign() < 0 {
		return NormalizedPrice{}, fmt.Errorf("nonnegative accrued interest required")
	}
	if !within(at, accrued.From, accrued.To) {
		out.Issues = append(out.Issues, "accrued_interest_outdated")
	}
	if accrued.Currency != terms.Currency {
		out.Issues = append(out.Issues, "contractual_accrued_conversion_required")
	}
	if len(out.Issues) > 0 {
		return out, nil
	}
	out.Accrued = amount
	out.Dirty = new(big.Rat).Add(clean, amount)
	return out, nil
}

// ValueUnits multiplies units, not lots. Unknown dirty value remains unknown,
// while a known clean component can still be displayed separately.
func (p NormalizedPrice) ValueUnits(quantity string) (NormalizedPrice, error) {
	q, err := number(quantity)
	if err != nil || q.Sign() < 0 {
		return NormalizedPrice{}, fmt.Errorf("nonnegative quantity required")
	}
	result := p
	result.Issues = append([]string{}, p.Issues...)
	multiply := func(v *big.Rat) *big.Rat {
		if v == nil {
			return nil
		}
		return new(big.Rat).Mul(v, q)
	}
	result.Clean = multiply(p.Clean)
	result.Accrued = multiply(p.Accrued)
	result.Dirty = multiply(p.Dirty)
	return result, nil
}
