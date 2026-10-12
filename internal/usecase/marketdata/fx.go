package marketdata

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

// FXPolicy is an operator-reviewed immutable convention. Changing its meaning
// requires a new version; fixings of older versions never migrate implicitly.
type FXPolicy struct {
	Alpha                                                                *AlphaFXPolicy    `json:",omitempty"`
	Official                                                             *OfficialFXPolicy `json:",omitempty"`
	Series                                                               []FXSeries        `json:",omitempty"`
	Key, Mode, Source, AccessScope                                       string
	SourceReference, TimeConvention, ValueConvention, ValidityConvention string
}
type SavedFXPolicy struct {
	FXPolicy
	Version    int64
	ActorID    string
	RecordedAt time.Time
}

func FXPolicyRef(key string, version int64) string { return fmt.Sprintf("%s@%d", key, version) }
func ValidateFXPolicy(p FXPolicy) error {
	if !scopePattern.MatchString(p.Key) || !scopePattern.MatchString(p.Source) || !scopePattern.MatchString(p.AccessScope) || (p.Mode != "market" && p.Mode != "cbr") {
		return ErrInvalid
	}
	for _, v := range []string{p.SourceReference, p.TimeConvention, p.ValueConvention, p.ValidityConvention} {
		if !mappingText(v, 2000) {
			return ErrInvalid
		}
	}
	if err := ValidateOfficialFXPolicy(p); err != nil {
		return err
	}
	if err := ValidateAlphaFXPolicy(p); err != nil {
		return err
	}
	return ValidateFXSeries(p)
}

type FXPoint struct {
	Withdrawn                   bool `json:",omitempty"`
	From, To, Rate              string
	At, AvailableAt, ValidUntil time.Time
	// A source document/response checksum and record locator identify the exact
	// evidence used by a trusted adapter, not merely a provider homepage.
	EvidenceSHA256, RecordReference string
}
type SavedFXPoint struct {
	PolicyKey     string
	PolicyVersion int64
	FXPoint
	ID         string
	Revision   int64
	RecordedAt time.Time
}

var fxDecimal = regexp.MustCompile(`^[0-9]{1,24}(\.[0-9]{1,18})?$`)
var evidenceHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func NormalizeFXPoint(p FXPoint) (FXPoint, error) {
	if !evidenceHash.MatchString(p.EvidenceSHA256) || !mappingText(p.RecordReference, 2000) {
		return p, ErrInvalid
	}
	for _, at := range []time.Time{p.At, p.AvailableAt, p.ValidUntil} {
		if at.IsZero() || at.Year() < 1900 || at.Year() > 2200 || at.Nanosecond()%1000 != 0 {
			return p, ErrInvalid
		}
	}
	// Monetary FX inputs allow more decimals than provider raw Quotation.
	if p.Withdrawn {
		if p.Rate != "" {
			return p, ErrInvalid
		}
		f := calc.FXFixing{From: p.From, To: p.To, Policy: "validation", ID: "point", Revision: 1, At: p.At, AvailableAt: p.AvailableAt, ValidUntil: p.ValidUntil, Withdrawn: true}
		if _, e := calc.NewFXResolver([]calc.FXFixing{f}, "validation", p.AvailableAt); e != nil {
			return p, ErrInvalid
		}
		p.At = p.At.UTC()
		p.AvailableAt = p.AvailableAt.UTC()
		p.ValidUntil = p.ValidUntil.UTC()
		return p, nil
	}
	if !fxDecimal.MatchString(p.Rate) {
		return p, ErrInvalid
	}
	p.Rate = strings.TrimLeft(p.Rate, "0")
	if p.Rate == "" || strings.HasPrefix(p.Rate, ".") {
		p.Rate = "0" + p.Rate
	}
	// Input scale is <=18, so this canonicalization is exact.
	n, _ := new(big.Rat).SetString(p.Rate)
	p.Rate = strings.TrimRight(strings.TrimRight(n.FloatString(18), "0"), ".")
	f := calc.FXFixing{From: p.From, To: p.To, Rate: p.Rate, Policy: "validation", ID: "point", Revision: 1, At: p.At, AvailableAt: p.AvailableAt, ValidUntil: p.ValidUntil}
	if _, e := calc.NewFXResolver([]calc.FXFixing{f}, "validation", p.AvailableAt); e != nil {
		return p, ErrInvalid
	}
	p.At = p.At.UTC()
	p.AvailableAt = p.AvailableAt.UTC()
	p.ValidUntil = p.ValidUntil.UTC()
	return p, nil
}
func (p SavedFXPoint) Fixing() calc.FXFixing {
	return calc.FXFixing{Withdrawn: p.Withdrawn, ID: p.ID, Revision: p.Revision, From: p.From, To: p.To, Rate: p.Rate, Policy: FXPolicyRef(p.PolicyKey, p.PolicyVersion), At: p.At, AvailableAt: p.AvailableAt, ValidUntil: p.ValidUntil}
}
