package investmentvaluation

import (
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"time"
)

const NativeBondPercentBasis = "native_bond_percent_v1"
const SplitInverseBasis = "split_inverse_v1"

// ReviewedBondNominalPoint pins a dated provider fact, not today's nominal.
type ReviewedBondNominalPoint struct {
	SnapshotID, SnapshotHash string
	Date                     time.Time
}

type ReviewedNativeBondBasis struct {
	Nominal, Currency string
	Points            []ReviewedBondNominalPoint
}

func (v ReviewedPriceAdjustment) basis() string {
	if v.Basis == "" {
		return SplitInverseBasis
	}
	return v.Basis
}

func validateNativeBondBasis(v ReviewedPriceAdjustment) error {
	n := v.NativeBond
	if v.Kind != "bond" || n == nil || v.Multiplier != "1" || v.Divisor != "1" || !SameAccruedAmount(v.Source.Price, v.ResolvedPrice) || !accruedDecimal.MatchString(n.Nominal) || SameAccruedAmount(n.Nominal, "0") || !accruedCurrency.MatchString(n.Currency) || (v.Source.Currency != "" && v.Source.Currency != n.Currency) || (v.Source.PriceUnit != "provider_unspecified" && v.Source.PriceUnit != "percent_of_nominal") {
		return d.ErrInvestmentInvalid
	}
	if !v.From.Equal(v.From.UTC().Truncate(24*time.Hour)) || !v.To.Equal(v.From.Add(24*time.Hour)) || v.From.Sub(v.Source.At) > 5*24*time.Hour || len(n.Points) < 2 || len(n.Points) > 6 {
		return d.ErrInvestmentInvalid
	}
	expected := v.Source.At
	for _, point := range n.Points {
		id, err := uuid.Parse(point.SnapshotID)
		if err != nil || id.String() != point.SnapshotID || !accruedHash.MatchString(point.SnapshotHash) || !point.Date.Equal(expected) || !conventionTime(point.Date) {
			return d.ErrInvestmentInvalid
		}
		expected = expected.Add(24 * time.Hour)
	}
	if !expected.Equal(v.From.Add(24 * time.Hour)) {
		return d.ErrInvestmentInvalid
	}
	return nil
}

func nativeBondTermsMatch(n *ReviewedNativeBondBasis, terms calc.PriceTerms) bool {
	return n != nil && terms.Kind == "bond" && terms.Unit == "percent_of_nominal" && terms.Currency == n.Currency && terms.NominalCurrency == n.Currency && SameAccruedAmount(terms.Nominal, n.Nominal)
}
