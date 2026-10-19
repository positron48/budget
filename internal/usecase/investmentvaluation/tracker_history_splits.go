package investmentvaluation

import (
	"math/big"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

// RebaseHistoricalPrice expresses a raw daily close in the recorded position's
// units. A split after the source candle day changes units, not total value.
// The observation remains unadjusted in storage. This is a retrospective book
// calculation, not evidence of corporate entitlement or historical tradability.
func RebaseHistoricalPrice(price *big.Rat, eligibleAt, at time.Time, key string, splits []d.InvestmentOperation) (*big.Rat, error) {
	// A reviewed zero estimate is usable portfolio evidence. Rebasing units
	// keeps that value zero, just as it preserves a positive quote's total value.
	if price == nil || price.Sign() < 0 || eligibleAt.IsZero() || at.Before(eligibleAt) || key == "" || reportingZoneError != nil {
		return nil, d.ErrInvestmentInvalid
	}
	out := new(big.Rat).Set(price)
	day := eligibleAt.UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
	for _, op := range splits {
		// Corporate dates follow the Moscow book calendar. Midnight Moscow is
		// 21:00 on the preceding UTC date; truncating that instant in UTC would
		// multiply a weekday pre-split close by the new quantity.
		local := op.OccurredAt.In(reportingZone)
		bookDay := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
		if op.Kind != "split" || op.Status != "posted" || op.InstrumentKey != key || op.OccurredAt.After(at) || !bookDay.After(day) {
			continue
		}
		old, ok1 := new(big.Rat).SetString(op.SplitOld)
		next, ok2 := new(big.Rat).SetString(op.SplitNew)
		if !ok1 || !ok2 || old.Sign() <= 0 || next.Sign() <= 0 {
			return nil, d.ErrInvestmentPrecondition
		}
		out.Mul(out, new(big.Rat).Quo(old, next))
	}
	return out, nil
}
