package investmentvaluation

import (
	"math/big"
	"slices"
	"strings"
	"time"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// This is an elapsed-time price age limit, not an exchange trading calendar.
const PortfolioQuoteMaxAge = 96 * time.Hour

type PortfolioQuote struct {
	InstrumentKey, Currency, Source, State string
	CleanPrice                             *big.Rat
	At                                     time.Time
	MappingVersion                         int64
	Issues                                 []string
	Instrument                             *md.Instrument
	MetadataAt                             time.Time
	RawPrice, Nominal, Accrued             *big.Rat
	RawUnit, NominalCurrency               string
	AccruedAt                              time.Time
}
type PortfolioQuotes struct {
	ReadAt time.Time
	Quotes []PortfolioQuote
}

// Estimates are usable for totals, but are never labelled exchange prices and
// cannot fill dates before their explicitly reviewed effective boundary.
func EstimatedPortfolioQuote(mapping md.Mapping, at time.Time) (PortfolioQuote, bool) {
	return portfolioPriceEstimate(mapping, at, false)
}

// Last-known estimates retain their observation date and only fill missing
// market valuations. Explicit manual estimates (including zero) take priority.
func WithPortfolioFallback(mapping md.Mapping, quote PortfolioQuote, at time.Time) PortfolioQuote {
	if UsablePortfolioQuote(quote) {
		return quote
	}
	if estimate, ok := portfolioPriceEstimate(mapping, at, true); ok {
		estimate.Instrument, estimate.MetadataAt = quote.Instrument, quote.MetadataAt
		return estimate
	}
	return quote
}

func portfolioPriceEstimate(mapping md.Mapping, at time.Time, fallback bool) (PortfolioQuote, bool) {
	if md.ValidatePriceEstimates(mapping.PriceEstimates) != nil {
		return PortfolioQuote{}, false
	}
	for i := len(mapping.PriceEstimates) - 1; i >= 0; i-- {
		e := mapping.PriceEstimates[i]
		if e.Fallback != fallback || at.Before(e.From) {
			continue
		}
		price, _ := new(big.Rat).SetString(e.Price)
		issue := "reviewed_price_estimate"
		if fallback {
			issue = "last_known_price"
		}
		return PortfolioQuote{InstrumentKey: mapping.InstrumentKey, MappingVersion: mapping.Version, Currency: e.Currency, CleanPrice: price, At: e.At, State: "estimated", Source: "manual_estimate", Issues: []string{issue}}, true
	}
	return PortfolioQuote{}, false
}

func UsablePortfolioQuote(q PortfolioQuote) bool {
	return q.CleanPrice != nil && (q.State == "fresh" || q.State == "estimated")
}

func NormalizeQuoteKeys(keys []string) ([]string, error) {
	if len(keys) > 500 {
		return nil, d.ErrInvestmentLimit
	}
	out := slices.Clone(keys)
	for _, key := range out {
		if key == "" || len(key) > 200 || strings.TrimSpace(key) != key || strings.ContainsRune(key, 0) {
			return nil, d.ErrInvestmentInvalid
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// NormalizePortfolioQuote exposes clean money per security. Missing bond NKD
// does not block a clean valuation, but missing/contradictory price terms do.
func NormalizePortfolioQuote(mapping md.Mapping, price md.SelectedPrice, at time.Time) (PortfolioQuote, error) {
	return NormalizePortfolioQuoteWithBondFacts(mapping, price, at, nil)
}

// A reviewed nominal captures the mapping's opening UTC day. Later percentage
// quotes require the provider's nominal for their own day, so amortization cannot
// silently reuse the original face value. This changes valuation, never the book.
func NormalizePortfolioQuoteWithBondFacts(mapping md.Mapping, price md.SelectedPrice, at time.Time, facts *md.SavedBondFacts) (PortfolioQuote, error) {
	return normalizePortfolioQuote(mapping, price, at, facts, at, false)
}

// Corrected retrospective history may use evidence retrieved later. Bond
// nominal belongs to the raw UTC candle day, not its next-day eligibility time.
func NormalizeHistoricalPortfolioQuote(mapping md.Mapping, price md.SelectedPrice, at, readAt time.Time, facts *md.SavedBondFacts) (PortfolioQuote, error) {
	if price.TimeConvention != md.RetrospectiveCloseNextUTCDay || readAt.Before(at) {
		return PortfolioQuote{}, d.ErrInvestmentInvalid
	}
	return normalizePortfolioQuote(mapping, price, at, facts, readAt, true)
}

func normalizePortfolioQuote(mapping md.Mapping, price md.SelectedPrice, at time.Time, facts *md.SavedBondFacts, knowledge time.Time, retrospective bool) (PortfolioQuote, error) {
	lastKnown := price.State == "stale" && price.Candidate != nil && len(price.Issues) == 1 && price.Issues[0] == "price_stale"
	if lastKnown {
		price.Fact, price.State, price.Issues = price.Candidate, "fresh", nil
	}
	out := PortfolioQuote{InstrumentKey: mapping.InstrumentKey, MappingVersion: mapping.Version, State: price.State, Issues: slices.Clone(price.Issues)}
	terms, ok := mapping.TermsAt(at)
	if retrospective {
		terms, ok = mapping.HistoricalTermsAt(at)
	}
	if !ok {
		out.State = "unavailable"
		out.Issues = append(out.Issues, "terms_missing")
		return out, nil
	}
	out.Currency, out.Source = terms.Currency, terms.SourceKey
	if price.Candidate != nil {
		out.At = price.Candidate.EffectiveAt
	}
	if price.State != "fresh" || price.Fact == nil {
		return out, nil
	}
	out.At = price.Fact.EffectiveAt
	if terms.Kind == "bond" && terms.Unit == "percent_of_nominal" {
		day := out.At.UTC().Truncate(24 * time.Hour)
		if retrospective {
			day = day.Add(-24 * time.Hour)
		}
		if facts != nil {
			nominal, ok := PortfolioBondNominal(facts, terms.InstrumentID, terms.Currency, day, knowledge)
			if !ok {
				out.State = "unavailable"
				out.Issues = append(out.Issues, "bond_nominal_unconfirmed")
				return out, nil
			}
			terms.Nominal, terms.NominalCurrency = nominal.Amount, nominal.Currency
		} else if !day.Equal(terms.From.UTC().Truncate(24 * time.Hour)) {
			out.State = "unavailable"
			out.Issues = append(out.Issues, "bond_nominal_unconfirmed")
			return out, nil
		}
	}
	normalized, e := calc.NormalizePrice(price.Fact, &terms, nil, at)
	if e != nil {
		out.State = "unavailable"
		out.Issues = append(out.Issues, "price_invalid")
		return out, nil
	}
	for _, issue := range normalized.Issues {
		if issue != "accrued_interest_missing" {
			out.Issues = append(out.Issues, issue)
		}
	}
	if len(out.Issues) > 0 || normalized.Clean == nil {
		out.State = "unavailable"
		return out, nil
	}
	if normalized.Clean.Sign() <= 0 {
		out.State = "unavailable"
		out.Issues = append(out.Issues, "nonpositive_exchange_price")
		return out, nil
	}
	out.CleanPrice = normalized.Clean
	out.RawPrice, _ = new(big.Rat).SetString(price.Fact.Price)
	out.RawUnit = terms.Unit
	if terms.Kind == "bond" && terms.Unit == "percent_of_nominal" {
		if nominal, ok := new(big.Rat).SetString(terms.Nominal); ok && nominal.Sign() > 0 {
			out.Nominal = nominal
			out.NominalCurrency = terms.NominalCurrency
		}
	}
	if lastKnown {
		out.State = "estimated"
		out.Issues = append(out.Issues, "last_known_price")
	}
	return out, nil
}

// PortfolioBondNominal verifies a nominal for one raw candle day. Current
// metadata alone never supplies an earlier historical face value.
func PortfolioBondNominal(f *md.SavedBondFacts, instrument, currency string, day, at time.Time) (md.BondAmount, bool) {
	if f.Instrument.ID != instrument || f.Instrument.Kind != "bond" || f.ID == "" || f.Hash == "" || f.RecordedAt.IsZero() || f.RecordedAt.After(at) || f.AsOf.IsZero() || f.AsOf.After(at) || day.Before(f.From) || day.After(f.To) || day.After(f.AsOf.UTC().Truncate(24*time.Hour)) || f.Terms.Nominal == nil || f.Terms.Nominal.Currency != currency {
		return md.BondAmount{}, false
	}
	var nominal *big.Rat
	var nominalText string
	for _, point := range f.Accrued {
		if !point.Date.UTC().Truncate(24*time.Hour).Equal(day) || point.Nominal == nil {
			continue
		}
		value, ok := new(big.Rat).SetString(*point.Nominal)
		if !ok || value.Sign() <= 0 || nominal != nil && nominal.Cmp(value) != 0 {
			return md.BondAmount{}, false
		}
		nominal = value
		nominalText = *point.Nominal
	}
	if nominal == nil {
		return md.BondAmount{}, false
	}
	if f.AsOf.UTC().Truncate(24 * time.Hour).Equal(day) {
		current, ok := new(big.Rat).SetString(f.Terms.Nominal.Amount)
		if !ok || current.Cmp(nominal) != 0 {
			return md.BondAmount{}, false
		}
	}
	return md.BondAmount{Amount: nominalText, Currency: currency}, true
}
