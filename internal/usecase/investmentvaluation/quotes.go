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
}
type PortfolioQuotes struct {
	ReadAt time.Time
	Quotes []PortfolioQuote
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
	out := PortfolioQuote{InstrumentKey: mapping.InstrumentKey, MappingVersion: mapping.Version, State: price.State, Issues: slices.Clone(price.Issues)}
	terms, ok := mapping.TermsAt(at)
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
	return out, nil
}
