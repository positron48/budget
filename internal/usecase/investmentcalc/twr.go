package investmentcalc

import (
	"fmt"
	"math/big"
	"time"
)

// PerformanceDay uses complete NAV and a signed external flow in the same
// currency, valued on that day's historical FX. Positive flow means contribution.
// Nil is unknown; it is never replaced by zero.
type PerformanceDay struct {
	Date              string
	NAV, ExternalFlow *big.Rat
	Complete          bool
}
type TWRPoint struct {
	Date, State, Reason        string
	Segment                    int
	DailyReturn, SegmentReturn *big.Rat
}
type TWRResult struct {
	Points []TWRPoint
	Return *big.Rat // absent across gaps/resets or without a measured interval
	Reason string
}

// DailyTWR implements the documented end-of-day-flow approximation. A return of
// 0.02 means 2%; money and chaining remain rational throughout.
func DailyTWR(days []PerformanceDay) (TWRResult, error) {
	out := TWRResult{Points: []TWRPoint{}}
	var previousNAV, product *big.Rat
	var previousDate time.Time
	segment, intervals := 0, 0
	broken := false
	for _, day := range days {
		date, e := time.Parse(time.DateOnly, day.Date)
		if e != nil || date.Format(time.DateOnly) != day.Date || !previousDate.IsZero() && !date.After(previousDate) {
			return out, fmt.Errorf("ordered unique reporting dates required")
		}
		point := TWRPoint{Date: day.Date}
		reason := ""
		if !previousDate.IsZero() && date.Sub(previousDate) != 24*time.Hour {
			broken = true
			previousNAV = nil
			product = nil
			point.Reason = "calendar_gap"
		}
		if !day.Complete || day.NAV == nil {
			reason = "nav_incomplete"
		} else if day.ExternalFlow == nil {
			reason = "external_flow_unknown"
		} else if day.NAV.Sign() < 0 {
			reason = "negative_nav"
		}
		previousDate = date
		if reason != "" {
			point.State = "gap"
			point.Reason = reason
			broken = true
			previousNAV = nil
			product = nil
			out.Points = append(out.Points, point)
			continue
		}
		if day.NAV.Sign() == 0 {
			point.State = "unfunded"
			if previousNAV != nil {
				point.State = "gap"
				point.Reason = "zero_nav"
				broken = true
			}
			previousNAV = nil
			product = nil
			out.Points = append(out.Points, point)
			continue
		}
		if previousNAV == nil {
			segment++
			product = big.NewRat(1, 1)
			previousNAV = new(big.Rat).Set(day.NAV)
			point.State = "baseline"
			point.Segment = segment
			out.Points = append(out.Points, point)
			continue
		}
		factor := new(big.Rat).Quo(new(big.Rat).Sub(day.NAV, day.ExternalFlow), previousNAV)
		if factor.Sign() <= 0 {
			point.State = "gap"
			point.Reason = "non_positive_return_factor"
			broken = true
			previousNAV = nil
			product = nil
			out.Points = append(out.Points, point)
			continue
		}
		product.Mul(product, factor)
		intervals++
		point.State = "calculated"
		point.Segment = segment
		point.DailyReturn = new(big.Rat).Sub(factor, big.NewRat(1, 1))
		point.SegmentReturn = new(big.Rat).Sub(product, big.NewRat(1, 1))
		previousNAV = new(big.Rat).Set(day.NAV)
		out.Points = append(out.Points, point)
	}
	switch {
	case len(days) == 0:
		out.Reason = "no_history"
	case broken || segment > 1:
		out.Reason = "discontinuous_history"
	case intervals == 0 || product == nil:
		out.Reason = "insufficient_history"
	default:
		out.Return = new(big.Rat).Sub(product, big.NewRat(1, 1))
	}
	return out, nil
}

// TWRIndex normalizes each independently measured segment to 100. A restart
// never inherits the previous segment's gain; gaps and unknown returns stay nil.
func TWRIndex(point TWRPoint) *big.Rat {
	if point.Segment < 1 {
		return nil
	}
	if point.State == "baseline" {
		return big.NewRat(100, 1)
	}
	if point.State != "calculated" || point.SegmentReturn == nil || point.SegmentReturn.Cmp(big.NewRat(-1, 1)) < 0 {
		return nil
	}
	return new(big.Rat).Mul(new(big.Rat).Add(point.SegmentReturn, big.NewRat(1, 1)), big.NewRat(100, 1))
}
