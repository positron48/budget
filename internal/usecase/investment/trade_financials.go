package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"strings"
	"time"
)

func tradeCurrency(v string) bool { return v == "RUB" || v == "USD" || v == "EUR" }
func tradeConverted(m d.InvestmentTradeMoney, base string) *big.Rat {
	v := rational(m.Amount)
	if m.Amount != "0" && m.Currency != base {
		v.Mul(v, new(big.Rat).Quo(rational(m.FXNumerator), rational(m.FXDenominator)))
	}
	return v
}

// The statement's main settlement contains only components with the same
// currency and effective instant. Other fees/taxes have their own cash legs.
func tradeDifference(o d.InvestmentOperation) *big.Rat {
	f := o.TradeFinancials
	expected := rational(f.Principal.Amount)
	for _, p := range []struct {
		m       d.InvestmentTradeMoney
		expense bool
	}{{f.Accrued, false}, {f.Fee, true}, {f.Tax, true}} {
		if p.expense && !f.ActualSettlementIncludesExpenses {
			continue
		}
		if p.m.Currency != f.Principal.Currency || !p.m.At.Equal(f.Principal.At) {
			continue
		}
		v := rational(p.m.Amount)
		if p.expense && o.Kind == "sell" {
			expected.Sub(expected, v)
		} else {
			expected.Add(expected, v)
		}
	}
	return new(big.Rat).Sub(rational(f.ActualSettlement), expected)
}
func tradeBookValues(o d.InvestmentOperation) (principal, accrued, fee *big.Rat) {
	if o.TradeFinancials == nil {
		return rational(o.Amount), rational(o.Accrued), rational(o.Fee)
	}
	f := o.TradeFinancials
	principal = tradeConverted(f.Principal, o.Currency)
	adjustment := tradeDifference(o)
	if f.Principal.Currency != o.Currency {
		adjustment.Mul(adjustment, new(big.Rat).Quo(rational(f.Principal.FXNumerator), rational(f.Principal.FXDenominator)))
	}
	principal.Add(principal, adjustment)
	return principal, tradeConverted(f.Accrued, o.Currency), tradeConverted(f.Fee, o.Currency)
}
func normalizeTradeFinancials(o *d.InvestmentOperation) error {
	if o.TradeFinancials == nil {
		return nil
	}
	if o.Kind != "buy" && o.Kind != "sell" {
		return invalid("trade financials only apply to buy/sell")
	}
	f := *o.TradeFinancials
	o.TradeFinancials = &f
	var e error
	for _, v := range []*string{&f.Price, &f.Nominal} {
		if *v == "" && v != &f.Nominal {
			return invalid("explicit price and actual settlement required")
		}
		*v, e = normalize(*v, 36, 12)
		if e != nil {
			return e
		}
	}
	if f.ActualSettlement == "" {
		return invalid("explicit actual settlement required")
	}
	negative := strings.HasPrefix(f.ActualSettlement, "-")
	f.ActualSettlement, e = normalize(strings.TrimPrefix(f.ActualSettlement, "-"), 36, 12)
	if e != nil {
		return e
	}
	if negative && f.ActualSettlement != "0" {
		f.ActualSettlement = "-" + f.ActualSettlement
	}
	if f.Price == "0" || f.PriceUnit != "currency_per_unit" && f.PriceUnit != "percent_nominal" || f.PriceUnit == "percent_nominal" && f.Nominal == "0" || f.PriceUnit == "currency_per_unit" && f.Nominal != "0" {
		return invalid("explicit clean quote unit and nominal required")
	}
	if len(f.DifferenceReason) > 2000 {
		return invalid("settlement difference reason too long")
	}
	if f.PriceCurrency == "" {
		f.PriceCurrency = o.Currency
	}
	quote := d.InvestmentTradeMoney{Amount: f.Price, Currency: f.PriceCurrency, At: o.OccurredAt, FXNumerator: f.PriceFXNumerator, FXDenominator: f.PriceFXDenominator, FXSource: f.PriceFXSource, FXEvidence: f.PriceFXEvidence, FXAt: f.PriceFXAt}
	for _, m := range []*d.InvestmentTradeMoney{&quote, &f.Principal, &f.Accrued, &f.Fee, &f.Tax} {
		m.Amount, e = normalize(m.Amount, 36, 12)
		if e != nil {
			return e
		}
		m.Currency = strings.ToUpper(strings.TrimSpace(m.Currency))
		if !tradeCurrency(m.Currency) {
			return invalid("component currency required")
		}
		if m.At.IsZero() {
			m.At = o.SettlementAt
		}
		m.At = m.At.UTC()
		if m.At.Nanosecond()%1000 != 0 || m.At.Before(o.OccurredAt) || m.At.Sub(o.OccurredAt) > 366*24*time.Hour {
			return invalid("component date must cover trade settlement")
		}
		if m == &quote && !m.At.Equal(o.OccurredAt) {
			return invalid("quote FX must use execution date")
		}
		if (m == &f.Principal || m == &f.Accrued) && !m.At.Equal(o.SettlementAt) {
			return invalid("principal and accrued must use settlement time")
		}
		if m.Currency == o.Currency || m.Amount == "0" {
			if m.FXNumerator != "" || m.FXDenominator != "" || m.FXSource != "" || m.FXEvidence != "" || !m.FXAt.IsZero() {
				return invalid("identity or zero component must not carry FX")
			}
		} else {
			for _, v := range []*string{&m.FXNumerator, &m.FXDenominator} {
				*v, e = normalize(*v, 36, 12)
				if e != nil || *v == "0" {
					return invalid("positive exact component FX ratio required")
				}
			}
			if m.FXAt.IsZero() {
				m.FXAt = m.At
			}
			m.FXAt = m.FXAt.UTC()
			if !m.FXAt.Equal(m.At) || strings.TrimSpace(m.FXSource) == "" || len(m.FXSource) > 200 || strings.TrimSpace(m.FXEvidence) == "" || len(m.FXEvidence) > 2000 {
				return invalid("FX date and explicit source evidence required")
			}
		}
	}
	f.PriceCurrency = quote.Currency
	f.PriceFXNumerator = quote.FXNumerator
	f.PriceFXDenominator = quote.FXDenominator
	f.PriceFXSource = quote.FXSource
	f.PriceFXEvidence = quote.FXEvidence
	f.PriceFXAt = quote.FXAt
	if f.Principal.Amount == "0" {
		return invalid("positive native clean principal required")
	}
	original := tradeConverted(f.Principal, o.Currency)
	expected := new(big.Rat).Mul(tradeConverted(quote, o.Currency), rational(o.Quantity))
	if f.PriceUnit == "percent_nominal" {
		expected.Mul(expected, new(big.Rat).Quo(rational(f.Nominal), big.NewRat(100, 1)))
	}
	delta := tradeDifference(*o)
	priceDifference := new(big.Rat).Sub(original, expected)
	calculated := new(big.Rat).Sub(rational(f.ActualSettlement), delta)
	for _, v := range []struct {
		field    *string
		expected string
	}{{&f.CalculatedSettlement, decimalString(calculated)}, {&f.SettlementDifference, decimalString(delta)}, {&f.PrincipalDifferenceNumerator, priceDifference.Num().String()}, {&f.PrincipalDifferenceDenominator, priceDifference.Denom().String()}} {
		if *v.field != "" && *v.field != v.expected {
			return invalid("trade comparison differs from native amounts")
		}
		*v.field = v.expected
	}
	if (original.Cmp(expected) != 0 || delta.Sign() != 0) && strings.TrimSpace(f.DifferenceReason) == "" {
		return invalid("price or actual settlement discrepancy requires reviewed reason and principal allocation")
	}
	principal, accrued, fee := tradeBookValues(*o)
	if principal.Sign() <= 0 {
		return invalid("settlement adjustment consumes clean principal")
	}
	values := []struct {
		field *string
		value *big.Rat
	}{{&o.Amount, principal}, {&o.Accrued, accrued}, {&o.Fee, fee}, {&o.Tax, tradeConverted(f.Tax, o.Currency)}}
	for _, v := range values {
		// Compatibility summaries use the existing display/storage scale. FIFO below
		// uses original amounts and exact FX ratios, never these rounded summaries.
		text := decimalString(v.value)
		text, e = normalize(text, 36, 12)
		if e != nil {
			return e
		}
		if *v.field != "0" && *v.field != text {
			return invalid("legacy summary contradicts native trade components")
		}
		*v.field = text
	}
	return nil
}

type tradeCashPart struct {
	currency, component, amount string
	at                          time.Time
}

func tradeCashParts(o d.InvestmentOperation) []tradeCashPart {
	f := o.TradeFinancials
	parts := []tradeCashPart{}
	for _, p := range []struct {
		m       d.InvestmentTradeMoney
		name    string
		expense bool
	}{{f.Principal, "principal", false}, {f.Accrued, "accrued", false}, {f.Fee, "fee", true}, {f.Tax, "tax", true}} {
		parts = append(parts, tradeCashPart{p.m.Currency, p.name, signed(p.m.Amount, p.expense || o.Kind == "buy"), p.m.At})
	}
	delta := tradeDifference(o)
	if o.Kind == "buy" {
		delta.Neg(delta)
	}
	parts = append(parts, tradeCashPart{f.Principal.Currency, "settlement_adjustment", decimalString(delta), o.SettlementAt})
	return parts
}
