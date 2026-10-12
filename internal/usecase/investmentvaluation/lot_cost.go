package investmentvaluation

import (
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"math/big"
	"time"
)

// LotCostSummary values are known subtotals. Completeness is independent for
// clean cost and acquired interest; nil is never interpreted as zero.
type LotCostSummary struct {
	Clean, Accrued                 *big.Rat
	CleanComplete, AccruedComplete bool
	Reasons                        []string
}

// LotCostConverter caches historical daily resolvers across positions. It uses
// the same Moscow day cutoff and explicit FX policy as portfolio valuation.
type LotCostConverter struct {
	Fixings []calc.FXFixing
	Policy  string
	cache   map[string]*calc.FXResolver
}

func (c *LotCostConverter) Convert(lots []inv.RemainingLot, from, to string) (LotCostSummary, error) {
	out := LotCostSummary{Clean: new(big.Rat), Accrued: new(big.Rat), CleanComplete: true, AccruedComplete: true}
	counts := [2]int{}
	addReason := func(reason string) {
		for _, old := range out.Reasons {
			if old == reason {
				return
			}
		}
		out.Reasons = append(out.Reasons, reason)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	for _, lot := range lots {
		for i, amount := range []*big.Rat{lot.CleanCost, lot.AcquiredAccrued} {
			var converted *big.Rat
			reason := ""
			switch {
			case amount == nil:
				reason = "book_value_unknown"
			case amount.Sign() == 0 || from == to:
				converted = new(big.Rat).Set(amount)
			case lot.AcquiredAt.IsZero():
				reason = "acquisition_date_missing"
			default:
				date := lot.AcquiredAt.In(loc).Format(time.DateOnly)
				cutoff, err := DayCutoff(date)
				if err != nil {
					return out, err
				}
				resolver, err := c.resolver(cutoff)
				if err != nil {
					return out, err
				}
				rate, ok := resolver.Resolve(from, to)
				if !ok {
					reason = "fx_missing"
				} else {
					converted = new(big.Rat).Mul(amount, rate.Rate)
				}
			}
			if reason != "" {
				addReason(reason)
				if i == 0 {
					out.CleanComplete = false
				} else {
					out.AccruedComplete = false
				}
				continue
			}
			counts[i]++
			if i == 0 {
				out.Clean.Add(out.Clean, converted)
			} else {
				out.Accrued.Add(out.Accrued, converted)
			}
		}
	}
	if len(lots) > 0 && counts[0] == 0 {
		out.Clean = nil
	}
	if len(lots) > 0 && counts[1] == 0 {
		out.Accrued = nil
	}
	return out, nil
}
