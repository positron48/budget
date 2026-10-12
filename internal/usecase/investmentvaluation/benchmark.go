package investmentvaluation

import (
	"fmt"
	"math/big"
	"time"

	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// BenchmarkConvention is operator-confirmed metadata, not a client-provided
// ticker label. A price index and an ETF proxy remain distinct products.
type BenchmarkConvention struct {
	Key, Title, Kind, Currency, SourceKey, InstrumentID, Unit, Evidence string
	Revision                                                            int64
	Confirmed                                                           bool
	From, To                                                            time.Time
}
type BenchmarkDailyEvidence struct {
	Date         string
	Price        md.SelectedPrice
	ExternalFlow *big.Rat // same display currency as the portfolio
}
type BenchmarkDailyProvenance struct {
	Date                string
	ObservationID       string
	ObservationRevision int64
	CalendarID          string
	CalendarRevision    int64
	FX                  []calc.FXLeg
	Issues              []string
}
type PreparedBenchmark struct {
	Convention BenchmarkConvention
	Currency   string
	Days       []calc.BenchmarkDay
	Evidence   []BenchmarkDailyProvenance
}

// PrepareBenchmark normalizes a previously selected daily price into historical
// display currency. Selection must have verified freshness/calendar beforehand;
// stale diagnostic candidates are never used. There is no current-FX fallback.
func PrepareBenchmark(convention BenchmarkConvention, currency, policy string, points []BenchmarkDailyEvidence, fixings []calc.FXFixing) (PreparedBenchmark, error) {
	out := PreparedBenchmark{Convention: convention, Currency: currency, Days: []calc.BenchmarkDay{}, Evidence: []BenchmarkDailyProvenance{}}
	validCurrency := func(s string) bool {
		return len(s) == 3 && s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'A' && s[1] <= 'Z' && s[2] >= 'A' && s[2] <= 'Z'
	}
	if !validCurrency(currency) || !validCurrency(convention.Currency) || policy == "" || convention.Key == "" || convention.Title == "" || convention.Revision < 1 || !convention.Confirmed || convention.Evidence == "" || convention.SourceKey == "" || convention.InstrumentID == "" || convention.From.IsZero() || !convention.To.IsZero() && !convention.To.After(convention.From) {
		return out, fmt.Errorf("confirmed benchmark convention required")
	}
	if convention.Kind != "price_index" && convention.Kind != "etf_price_proxy" {
		return out, fmt.Errorf("unsupported benchmark series type")
	}
	if convention.Kind == "price_index" && convention.Unit != "index_points" || convention.Kind == "etf_price_proxy" && convention.Unit != "currency_per_unit" {
		return out, fmt.Errorf("benchmark unit conflicts with type")
	}
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return out, err
	}
	var previous string
	for _, p := range points {
		cutoff, e := DayCutoff(p.Date)
		if e != nil || previous != "" && p.Date <= previous {
			return out, fmt.Errorf("ordered benchmark dates required")
		}
		previous = p.Date
		day := calc.BenchmarkDay{Date: p.Date}
		if p.ExternalFlow != nil {
			day.ExternalFlow = new(big.Rat).Set(p.ExternalFlow)
		}
		provenance := BenchmarkDailyProvenance{Date: p.Date, ObservationID: p.Price.ObservationID, ObservationRevision: p.Price.ObservationRevision, CalendarID: p.Price.CalendarID, CalendarRevision: p.Price.CalendarRevision, Issues: []string{}}
		quote := p.Price.Fact
		switch {
		case cutoff.Before(convention.From) || !convention.To.IsZero() && !cutoff.Before(convention.To):
			provenance.Issues = append(provenance.Issues, "benchmark_convention_coverage_missing")
		case p.Price.State != "fresh" || quote == nil:
			provenance.Issues = append(provenance.Issues, p.Price.Issues...)
			provenance.Issues = append(provenance.Issues, "benchmark_price_unavailable")
		case p.Price.ObservationID == "" || p.Price.ObservationRevision < 1 || p.Price.CalendarID == "" || p.Price.CalendarRevision < 1:
			provenance.Issues = append(provenance.Issues, "benchmark_calendar_evidence_missing")
		default:
			if quote.InstrumentID != convention.InstrumentID || quote.SourceKey != convention.SourceKey {
				return out, fmt.Errorf("benchmark source identity mismatch")
			}
			if quote.EffectiveAt.IsZero() || quote.AvailableAt.IsZero() || quote.AvailableAt.Before(quote.EffectiveAt) {
				return out, fmt.Errorf("invalid benchmark timestamps")
			}
			if quote.EffectiveAt.Before(convention.From) || !convention.To.IsZero() && !quote.EffectiveAt.Before(convention.To) {
				provenance.Issues = append(provenance.Issues, "benchmark_convention_coverage_missing")
			}
			if !quote.Complete || quote.EffectiveAt.After(cutoff) || quote.AvailableAt.After(cutoff) {
				provenance.Issues = append(provenance.Issues, "benchmark_price_unavailable")
			}
			if quote.Adjustment != "unadjusted" {
				provenance.Issues = append(provenance.Issues, "benchmark_adjustment_unconfirmed")
			}
			if quote.Currency != "" && quote.Currency != convention.Currency || quote.Unit != "" && quote.Unit != "provider_unspecified" && quote.Unit != convention.Unit {
				provenance.Issues = append(provenance.Issues, "benchmark_price_convention_conflict")
			}
			native, ok := new(big.Rat).SetString(quote.Price)
			if !ok || native.Sign() <= 0 {
				return out, fmt.Errorf("positive benchmark price required")
			}
			if len(provenance.Issues) == 0 {
				resolver, e := calc.NewFXResolver(fixings, policy, cutoff)
				if e != nil {
					return out, e
				}
				fx, found := resolver.Resolve(convention.Currency, currency)
				if !found {
					provenance.Issues = append(provenance.Issues, "benchmark_fx_missing")
				} else {
					day.Price = new(big.Rat).Mul(native, fx.Rate)
					provenance.FX = fx.Legs
					// A carried close is valid valuation evidence, never today's execution.
					day.CanTrade = quote.EffectiveAt.In(location).Format(time.DateOnly) == p.Date && quote.AvailableAt.In(location).Format(time.DateOnly) == p.Date
				}
			}
		}
		out.Days = append(out.Days, day)
		out.Evidence = append(out.Evidence, provenance)
	}
	return out, nil
}
