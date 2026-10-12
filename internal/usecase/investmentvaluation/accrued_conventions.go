package investmentvaluation

import (
	"fmt"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"math/big"
	"regexp"
	"time"
)

// ReviewedAccruedPeriod confirms the currency and validity of one immutable
// provider point. The worker cannot infer either from present-day bond metadata.
// Amount and pinned provenance are retained in the reviewed profile for replay.
type ReviewedAccruedPeriod struct {
	md.Scope
	InstrumentID, SnapshotID, SnapshotHash string
	PointAt                                time.Time
	From, To                               time.Time // explicit half-open interval, at most 48 hours
	Currency, AmountPerUnit, Evidence      string
}

var accruedDecimal = regexp.MustCompile(`^[0-9]{1,24}(\.[0-9]{1,9})?$`)
var accruedCurrency = regexp.MustCompile(`^[A-Z]{3}$`)
var accruedHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validateAccruedPeriods(tenant string, p ConventionProfile) error {
	if len(p.Accrued) > 500 {
		return d.ErrInvestmentInvalid
	}
	total := 0
	for key, periods := range p.Accrued {
		if !conventionText(key, 200) || len(periods) == 0 {
			return d.ErrInvestmentInvalid
		}
		total += len(periods)
		if total > 10000 {
			return d.ErrInvestmentLimit
		}
		for i, v := range periods {
			if v.TenantID != tenant || md.ValidateScope(v.Scope) != nil || !conventionTime(v.PointAt) || !conventionTime(v.From) || !conventionTime(v.To) || !v.To.After(v.From) || v.To.Sub(v.From) > 48*time.Hour || v.PointAt.Before(v.From) || !v.PointAt.Before(v.To) || i > 0 && v.From.Before(periods[i-1].To) || !conventionText(v.Evidence, 2000) || !accruedHash.MatchString(v.SnapshotHash) || !accruedCurrency.MatchString(v.Currency) || !accruedDecimal.MatchString(v.AmountPerUnit) {
				return d.ErrInvestmentInvalid
			}
			for _, id := range []string{v.InstrumentID, v.SnapshotID} {
				u, e := uuid.Parse(id)
				if e != nil || u.String() != id {
					return d.ErrInvestmentInvalid
				}
			}
			compatible := false
			for _, price := range p.Periods[key] {
				if price.Scope == v.Scope && price.InstrumentID == v.InstrumentID && !v.From.Before(price.From) && (price.To.IsZero() || !v.To.After(price.To)) {
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
func (p ConventionProfile) AccruedAt(key string, at time.Time, scope md.Scope, id string, version int64) *calc.AccruedFact {
	if !p.Enabled || version < 1 {
		return nil
	}
	for _, v := range p.Accrued[key] {
		if !at.Before(v.From) && at.Before(v.To) {
			if v.Scope != scope || v.InstrumentID != id {
				return nil
			}
			return &calc.AccruedFact{InstrumentID: id, SourceID: fmt.Sprintf("%s:%d/%s/%s/%s", p.Key, version, v.SnapshotID, v.SnapshotHash, v.PointAt.UTC().Format(time.RFC3339Nano)), Currency: v.Currency, AmountPerUnit: v.AmountPerUnit, From: v.From, To: v.To}
		}
	}
	return nil
}
func SameAccruedAmount(a, b string) bool {
	x, ok := new(big.Rat).SetString(a)
	y, yes := new(big.Rat).SetString(b)
	return ok && yes && x.Cmp(y) == 0
}
