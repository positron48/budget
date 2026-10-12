package investmentvaluation

import (
	"github.com/google/uuid"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"strings"
	"testing"
	"time"
)

func nativeBondFixture(t *testing.T) (ConventionProfile, md.PriceSelectionRequest, []md.ScopedObservation, calc.PriceTerms) {
	t.Helper()
	p, q, input := adjustmentFixture(t)
	o := input[0].Observation
	o.Price = "95.000000001"
	o.Open = o.Price
	o.Low = o.Price
	o.High = o.Price
	o.Currency = ""
	o.PriceUnit = "provider_unspecified"
	input[0].Observation = o
	v := p.Adjustments["S"][0]
	v.Source = o
	v.ObservationHash = PriceObservationHash(o)
	v.Basis = NativeBondPercentBasis
	v.Kind = "bond"
	v.Multiplier = "1"
	v.Divisor = "1"
	v.ResolvedPrice = o.Price
	v.NativeBond = &ReviewedNativeBondBasis{Nominal: "500", Currency: "RUB", Points: []ReviewedBondNominalPoint{{SnapshotID: uuid.NewString(), SnapshotHash: strings.Repeat("b", 64), Date: o.At}, {SnapshotID: uuid.NewString(), SnapshotHash: strings.Repeat("c", 64), Date: v.From}}}
	p.Adjustments["S"][0] = v
	terms := calc.PriceTerms{ID: "dated", Revision: 1, InstrumentID: o.InstrumentID, SourceKey: q.Provider + "/" + q.AccessScope, Kind: "bond", Currency: "RUB", Unit: "percent_of_nominal", Nominal: "500", NominalCurrency: "RUB", From: o.At, Confirmed: true}
	return p, q, input, terms
}

func TestNativeBondBasisKeepsPercentAndDatedNominal(t *testing.T) {
	p, q, input, terms := nativeBondFixture(t)
	if err := ValidateConventionProfile(q.TenantID, p); err != nil {
		t.Fatal(err)
	}
	raw, err := md.SelectValuationPrice(q, input)
	if err != nil {
		t.Fatal(err)
	}
	selected := p.AdjustSelectedPrice("S", q.At, q.Scope, terms, raw, input, 1)
	if selected.Fact == nil || selected.Fact.Price != raw.Fact.Price || selected.Candidate.Adjustment != "provider_unspecified" || selected.AdjustmentProof.Basis != NativeBondPercentBasis || selected.AdjustmentProof.Nominal != "500" {
		t.Fatal(selected)
	}
	accrued := &calc.AccruedFact{InstrumentID: terms.InstrumentID, SourceID: "dated-accrued", Currency: "RUB", AmountPerUnit: "5.000000001", From: terms.From, To: q.At.Add(time.Hour)}
	got, err := calc.NormalizePrice(selected.Fact, &terms, accrued, q.At)
	if err != nil || !got.Complete() || got.Clean.FloatString(9) != "475.000000005" || got.Dirty.FloatString(9) != "480.000000006" {
		t.Fatal(got, err)
	}
	for _, change := range []func(*calc.PriceTerms){func(v *calc.PriceTerms) { v.Nominal = "1000" }, func(v *calc.PriceTerms) { v.NominalCurrency = "USD" }, func(v *calc.PriceTerms) { v.Unit = "currency_per_unit" }} {
		bad := terms
		change(&bad)
		if got := p.AdjustSelectedPrice("S", q.At, q.Scope, bad, raw, input, 1); got.State != "partial" || got.Fact != nil {
			t.Fatal("nominal/unit conflict promoted", got)
		}
	}
}

func TestNativeBondBasisRejectsMissingDatesAndNonIdentityFactors(t *testing.T) {
	for _, change := range []func(*ReviewedPriceAdjustment){
		func(v *ReviewedPriceAdjustment) { v.NativeBond.Points = v.NativeBond.Points[1:] },
		func(v *ReviewedPriceAdjustment) { v.NativeBond.Points[1].Date = v.From.Add(time.Hour) },
		func(v *ReviewedPriceAdjustment) { v.NativeBond.Points[0].SnapshotHash = "" },
		func(v *ReviewedPriceAdjustment) { v.NativeBond.Nominal = "0" },
		func(v *ReviewedPriceAdjustment) {
			v.NativeBond.Currency = "USD"
			v.Source.Currency = "RUB"
			v.ObservationHash = PriceObservationHash(v.Source)
		},
		func(v *ReviewedPriceAdjustment) { v.Multiplier = "10"; v.ResolvedPrice = "950.000000010" },
		func(v *ReviewedPriceAdjustment) { v.Basis = SplitInverseBasis },
		func(v *ReviewedPriceAdjustment) { v.Kind = "share" },
	} {
		p, q, _, _ := nativeBondFixture(t)
		v := p.Adjustments["S"][0]
		change(&v)
		p.Adjustments["S"][0] = v
		if ValidateConventionProfile(q.TenantID, p) == nil {
			t.Fatal("invalid native bond review accepted", v)
		}
	}
}
