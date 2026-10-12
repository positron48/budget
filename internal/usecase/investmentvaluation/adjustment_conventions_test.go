package investmentvaluation

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func adjustmentFixture(t *testing.T) (ConventionProfile, md.PriceSelectionRequest, []md.ScopedObservation) {
	t.Helper()
	scope := md.Scope{TenantID: uuid.NewString(), Provider: "tinvest", AccessScope: "personal"}
	day := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	source, err := md.Normalize(md.Observation{InstrumentID: uuid.NewString(), Kind: "daily_candle", At: day, SourceKind: "exchange", Adjustment: "provider_unspecified", PriceUnit: "currency_per_unit", Currency: "RUB", Price: "900719925474099.123456789", Open: "900719925474099.123456789", High: "900719925474099.123456789", Low: "900719925474099.123456789", Volume: "1", Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	q := md.PriceSelectionRequest{Scope: scope, InstrumentID: source.InstrumentID, Kind: source.Kind, SourceKind: source.SourceKind, Adjustment: source.Adjustment, At: day.Add(36 * time.Hour), Retrospective: &md.RetrospectiveDailyPolicy{Convention: md.RetrospectiveCloseNextUTCDay, MaxAge: 96 * time.Hour}}
	observations := []md.ScopedObservation{{Scope: scope, SavedObservation: md.SavedObservation{Observation: source, ID: uuid.NewString(), Revision: 2, ReceivedAt: day.AddDate(1, 0, 0)}}}
	v := ReviewedPriceAdjustment{Scope: scope, ObservationID: observations[0].ID, ObservationRevision: 2, ObservationHash: PriceObservationHash(source), Source: source, From: day.Add(24 * time.Hour), To: day.Add(48 * time.Hour), Kind: "share", Multiplier: "10", Divisor: "1", ResolvedPrice: "9007199254740991.234567890", SourceReference: "https://issuer.example.test/split", EvidenceSHA256: strings.Repeat("a", 64), Evidence: "Synthetic issuer split and pinned provider adjustment basis"}
	p := ConventionProfile{Key: "adjusted", Title: "Synthetic exact inversion", Evidence: "Synthetic review", Enabled: true, Periods: map[string][]PriceConventionPeriod{"S": {{Scope: scope, InstrumentID: source.InstrumentID, From: v.From, To: v.To, Policy: SeriesPolicy{Kind: q.Kind, SourceKind: q.SourceKind, Adjustment: q.Adjustment, Retrospective: q.Retrospective}}}}, Adjustments: map[string][]ReviewedPriceAdjustment{"S": {v}}}
	return p, q, observations
}

func TestReviewedAdjustmentExactInversionAndRestoredIdentity(t *testing.T) {
	p, q, observations := adjustmentFixture(t)
	if err := ValidateConventionProfile(q.TenantID, p); err != nil {
		t.Fatal(err)
	}
	for _, restore := range []bool{false, true} {
		if restore {
			observations[0].ID = uuid.NewString()
			observations[0].Revision = 1
		}
		raw, err := md.SelectValuationPrice(q, observations)
		if err != nil {
			t.Fatal(err)
		}
		got := p.AdjustSelectedPrice("S", q.At, q.Scope, calc.PriceTerms{Kind: "share"}, raw, observations, 3)
		if got.Fact == nil || got.Fact.Price != "9007199254740991.234567890" || got.Fact.Adjustment != "unadjusted" || got.AdjustmentProof == nil || got.AdjustmentProof.ProfileVersion != 3 || got.AdjustmentProof.ObservationRevision != 2 {
			t.Fatal(got)
		}
		if got.Candidate.Price != observations[0].Price || raw.Fact.Adjustment != "provider_unspecified" || got.Fact.EffectiveAt != raw.Fact.EffectiveAt || got.CalendarID != "" {
			t.Fatal("source/time evidence changed", got)
		}
	}
}

func TestReviewedAdjustmentReverseSplitIsExactWithoutRounding(t *testing.T) {
	p, q, observations := adjustmentFixture(t)
	review := p.Adjustments["S"][0]
	raw, _ := new(big.Rat).SetString(review.Source.Price)
	review.Multiplier = "1"
	review.Divisor = "5000"
	review.ResolvedPrice = new(big.Rat).Quo(raw, big.NewRat(5000, 1)).FloatString(18)
	p.Adjustments["S"][0] = review
	if err := ValidateConventionProfile(q.TenantID, p); err != nil {
		t.Fatal(err)
	}
	selected, err := md.SelectValuationPrice(q, observations)
	if err != nil {
		t.Fatal(err)
	}
	got := p.AdjustSelectedPrice("S", q.At, q.Scope, calc.PriceTerms{Kind: "share"}, selected, observations, 1)
	if got.Fact == nil || got.Fact.Price != review.ResolvedPrice || got.AdjustmentProof.Divisor != "5000" {
		t.Fatal(got)
	}
}

func TestReviewedAdjustmentRejectsIncorrectExactFactorAndBinding(t *testing.T) {
	for _, change := range []func(*ReviewedPriceAdjustment){
		func(v *ReviewedPriceAdjustment) { v.ResolvedPrice = "9007199254740991.234567891" },
		func(v *ReviewedPriceAdjustment) { v.Multiplier = "0" },
		func(v *ReviewedPriceAdjustment) { v.Divisor = "3" },
		func(v *ReviewedPriceAdjustment) { v.ObservationHash = strings.Repeat("0", 64) },
		func(v *ReviewedPriceAdjustment) { v.EvidenceSHA256 = "" },
		func(v *ReviewedPriceAdjustment) { v.SourceReference = "" },
		func(v *ReviewedPriceAdjustment) { v.TenantID = uuid.NewString() },
		func(v *ReviewedPriceAdjustment) { v.Kind = "bond" },
		func(v *ReviewedPriceAdjustment) { v.To = v.To.AddDate(0, 2, 0) },
		func(v *ReviewedPriceAdjustment) { v.From = v.Source.At },
	} {
		p, q, _ := adjustmentFixture(t)
		v := p.Adjustments["S"][0]
		change(&v)
		p.Adjustments["S"][0] = v
		if ValidateConventionProfile(q.TenantID, p) == nil {
			t.Fatal("invalid review accepted", v)
		}
	}
}

func TestReviewedAdjustmentCannotPromoteChangedStaleOrForeignCandle(t *testing.T) {
	for _, scenario := range []string{"price", "ohlc", "scope", "stale", "future", "unreviewed", "boundary", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			p, q, observations := adjustmentFixture(t)
			switch scenario {
			case "price":
				observations[0].Price = "1"
				observations[0].Low = "1"
			case "ohlc":
				observations[0].Volume = "2"
			case "scope":
				p.Adjustments["S"][0].AccessScope = "other"
			case "stale":
				q.At = q.At.AddDate(0, 0, 5)
			case "future":
				q.At = observations[0].At.Add(time.Hour)
			case "unreviewed":
				p.Adjustments = nil
			case "boundary":
				q.At = p.Adjustments["S"][0].To
			case "disabled":
				p.Enabled = false
			}
			raw, err := md.SelectValuationPrice(q, observations)
			if err != nil {
				t.Fatal(err)
			}
			got := p.AdjustSelectedPrice("S", q.At, q.Scope, calc.PriceTerms{Kind: "share"}, raw, observations, 3)
			if got.AdjustmentProof != nil || got.Fact != nil && got.Fact.Adjustment == "unadjusted" {
				t.Fatal("unknown adjusted quote promoted", got)
			}
			if scenario == "price" || scenario == "ohlc" || scenario == "scope" {
				if got.State != "partial" || got.Fact != nil || len(got.Issues) == 0 {
					t.Fatal("changed review not blocked", got)
				}
			}
		})
	}
}
