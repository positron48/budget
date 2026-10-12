package investmentvaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// ReviewedPriceAdjustment resolves one pinned daily observation, never an entire
// provider series. Corporate evidence and the source snapshot are both retained
// in the canonical profile; a later provider correction requires a new review.
type ReviewedPriceAdjustment struct {
	md.Scope
	Basis                                     string                   `json:",omitempty"`
	NativeBond                                *ReviewedNativeBondBasis `json:",omitempty"`
	ObservationID                             string
	ObservationRevision                       int64
	ObservationHash                           string
	Source                                    md.Observation
	From, To                                  time.Time
	Kind, Multiplier, Divisor, ResolvedPrice  string
	SourceReference, EvidenceSHA256, Evidence string
}

var adjustmentInteger = regexp.MustCompile(`^[1-9][0-9]{0,11}$`)
var adjustmentDecimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,23})(\.[0-9]{1,18})?$`)

func PriceObservationHash(o md.Observation) string {
	raw, _ := json.Marshal(o) // Observation contains only JSON-compatible fields.
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func validateAdjustmentPeriods(tenant string, p ConventionProfile) error {
	if len(p.Adjustments) > 500 {
		return d.ErrInvestmentLimit
	}
	total := 0
	for key, periods := range p.Adjustments {
		if !conventionText(key, 200) || len(periods) == 0 {
			return d.ErrInvestmentInvalid
		}
		total += len(periods)
		if total > 10000 {
			return d.ErrInvestmentLimit
		}
		for i, v := range periods {
			id, e := uuid.Parse(v.ObservationID)
			normalized, err := md.Normalize(v.Source)
			if e != nil || id.String() != v.ObservationID || v.ObservationRevision < 1 || v.TenantID != tenant || md.ValidateScope(v.Scope) != nil || err != nil || normalized != v.Source || v.ObservationHash != PriceObservationHash(v.Source) || !accruedHash.MatchString(v.ObservationHash) || !accruedHash.MatchString(v.EvidenceSHA256) || !conventionText(v.SourceReference, 2000) || !conventionText(v.Evidence, 2000) || !conventionTime(v.From) || !conventionTime(v.To) || !v.To.After(v.From) || v.To.Sub(v.From) > 30*24*time.Hour || i > 0 && v.From.Before(periods[i-1].To) {
				return d.ErrInvestmentInvalid
			}
			if v.Source.Kind != "daily_candle" || v.Source.SourceKind != "exchange" || v.Source.Adjustment != "provider_unspecified" || !v.Source.Complete || !v.Source.At.Equal(v.Source.At.UTC().Truncate(24*time.Hour)) || v.Source.At.Add(24*time.Hour).After(v.From) || !adjustmentInteger.MatchString(v.Multiplier) || !adjustmentInteger.MatchString(v.Divisor) || !adjustmentDecimal.MatchString(v.ResolvedPrice) {
				return d.ErrInvestmentInvalid
			}
			switch v.basis() {
			case SplitInverseBasis:
				if (v.Kind != "share" && v.Kind != "etf") || v.NativeBond != nil {
					return d.ErrInvestmentInvalid
				}
			case NativeBondPercentBasis:
				if err := validateNativeBondBasis(v); err != nil {
					return err
				}
			default:
				return d.ErrInvestmentInvalid
			}
			source, _ := new(big.Rat).SetString(v.Source.Price)
			multiplier, _ := new(big.Rat).SetString(v.Multiplier)
			divisor, _ := new(big.Rat).SetString(v.Divisor)
			resolved, _ := new(big.Rat).SetString(v.ResolvedPrice)
			if source.Sign() <= 0 || resolved.Sign() <= 0 || new(big.Rat).Quo(new(big.Rat).Mul(source, multiplier), divisor).Cmp(resolved) != 0 {
				return d.ErrInvestmentInvalid
			}
			compatible := false
			for _, period := range p.Periods[key] {
				if period.Scope == v.Scope && period.InstrumentID == v.Source.InstrumentID && period.Policy.Kind == v.Source.Kind && period.Policy.SourceKind == v.Source.SourceKind && period.Policy.Adjustment == v.Source.Adjustment && !v.From.Before(period.From) && (period.To.IsZero() || !v.To.After(period.To)) {
					compatible = true
					break
				}
			}
			if !compatible {
				return d.ErrInvestmentInvalid
			}
		}
	}
	return nil
}

// AdjustSelectedPrice preserves the raw Candidate and time/quality checks. It
// cannot promote a stale, partial, future, different-source or changed candle.
// Content identity also permits explicit cache rehydration after archive restore
// under a new DB-local ID, while retaining the originally approved ID/revision.
func (p ConventionProfile) AdjustSelectedPrice(key string, at time.Time, scope md.Scope, terms calc.PriceTerms, selected md.SelectedPrice, observations []md.ScopedObservation, version int64) md.SelectedPrice {
	if !p.Enabled || version < 1 || selected.State != "fresh" || len(selected.Issues) > 0 || selected.Fact == nil || selected.Fact.Adjustment != "provider_unspecified" {
		return selected
	}
	for _, v := range p.Adjustments[key] {
		if at.Before(v.From) || !at.Before(v.To) {
			continue
		}
		matches := v.Scope == scope && v.Kind == terms.Kind && v.Source.InstrumentID == selected.Fact.InstrumentID && selected.Fact.SourceKey == scope.Provider+"/"+scope.AccessScope && selected.Fact.Price == v.Source.Price
		if v.basis() == NativeBondPercentBasis {
			matches = matches && nativeBondTermsMatch(v.NativeBond, terms)
		}
		found := false
		for _, o := range observations {
			if o.ID == selected.ObservationID && o.Revision == selected.ObservationRevision && o.Scope == scope && PriceObservationHash(o.Observation) == v.ObservationHash {
				found = true
				break
			}
		}
		if !matches || !found {
			selected.Issues = append(append([]string{}, selected.Issues...), "price_adjustment_review_mismatch")
			selected.State = "partial"
			selected.Fact = nil
			return selected
		}
		quote := *selected.Fact
		quote.Price = v.ResolvedPrice
		quote.Adjustment = "unadjusted"
		selected.Fact = &quote
		selected.AdjustmentProof = &calc.PriceAdjustmentProof{Basis: v.basis(), ProfileKey: p.Key, ProfileVersion: version, ObservationID: v.ObservationID, ObservationRevision: v.ObservationRevision, ObservationHash: v.ObservationHash, SourcePrice: v.Source.Price, Multiplier: v.Multiplier, Divisor: v.Divisor, ResolvedPrice: v.ResolvedPrice, SourceReference: v.SourceReference, EvidenceSHA256: v.EvidenceSHA256, Reference: fmt.Sprintf("%s:v%d/%s", p.Key, version, v.ObservationHash)}
		if v.NativeBond != nil {
			selected.AdjustmentProof.Nominal = v.NativeBond.Nominal
			selected.AdjustmentProof.NominalCurrency = v.NativeBond.Currency
		}
		return selected
	}
	return selected
}
