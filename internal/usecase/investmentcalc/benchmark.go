package investmentcalc

import (
	"fmt"
	"math/big"
	"time"
)

// BenchmarkDay.Price is already normalized into the portfolio's display
// currency using that day's FX. CanTrade distinguishes a new executable session
// from an eligible carried valuation. Nil prices are gaps, not zero prices.
type BenchmarkDay struct {
	Date                string
	Price, ExternalFlow *big.Rat
	CanTrade            bool
	HistoryReset        bool
}
type BenchmarkPoint struct {
	Date, State, Reason                          string
	Index, Value, Units, Cash, PendingWithdrawal *big.Rat
}
type BenchmarkResult struct {
	Points []BenchmarkPoint
	Reason string
}

// BenchmarkPortfolio simulates fractional units without transaction costs.
// The first day seeds virtual units at the shared eligible valuation price,
// including a verified carried close; it ignores baseline flows. Subsequent
// cash-flow executions still require a new executable session.
// A withdrawal without an executable quote waits as a separately disclosed
// liability. Its eventual execution is never backdated to the missing day.
// Price/TR/proxy identity and evidence selection are established upstream; this
// function never fabricates dividends on top of the supplied benchmark series.
func BenchmarkPortfolio(currency string, initialNAV *big.Rat, days []BenchmarkDay) (BenchmarkResult, error) {
	out := BenchmarkResult{Points: []BenchmarkPoint{}}
	if !validMoneyCurrency(currency) || initialNAV == nil || initialNAV.Sign() < 0 {
		return out, fmt.Errorf("known nonnegative baseline and display currency required")
	}
	var previous time.Time
	for i, day := range days {
		date, e := time.Parse(time.DateOnly, day.Date)
		if e != nil || date.Format(time.DateOnly) != day.Date || i > 0 && date.Unix()-previous.Unix() != 86400 {
			return out, fmt.Errorf("consecutive reporting dates required")
		}
		if day.Price != nil && day.Price.Sign() <= 0 {
			return out, fmt.Errorf("positive benchmark price required")
		}
		if day.CanTrade && day.Price == nil {
			return out, fmt.Errorf("execution requires known price")
		}
		previous = date
	}
	if len(days) == 0 {
		out.Reason = "no_history"
		return out, nil
	}
	cash := new(big.Rat)
	units := new(big.Rat)
	pending := new(big.Rat)
	var baseline *big.Rat
	stopped := ""
	if days[0].Price == nil {
		stopped = "initial_benchmark_price_unavailable"
	} else {
		baseline = new(big.Rat).Set(days[0].Price)
		units.Quo(initialNAV, baseline)
	}
	for i, day := range days {
		point := BenchmarkPoint{Date: day.Date}
		if baseline != nil && day.Price != nil {
			point.Index = new(big.Rat).Mul(new(big.Rat).Quo(day.Price, baseline), big.NewRat(100, 1))
		}
		if stopped != "" {
			point.State = "stopped"
			point.Reason = stopped
			out.Points = append(out.Points, point)
			continue
		}
		if i > 0 {
			if day.HistoryReset {
				stopped = "history_reset"
				point.State = "stopped"
				point.Reason = stopped
				out.Points = append(out.Points, point)
				continue
			}
			if day.ExternalFlow == nil {
				stopped = "external_flow_unknown"
				point.State = "stopped"
				point.Reason = stopped
				out.Points = append(out.Points, point)
				continue
			}
			// Net daily payments first consume cash or reduce an unpaid withdrawal.
			balance := new(big.Rat).Sub(new(big.Rat).Add(cash, day.ExternalFlow), pending)
			cash.SetInt64(0)
			pending.SetInt64(0)
			if balance.Sign() < 0 {
				pending.Neg(balance)
			} else {
				cash.Set(balance)
			}
			if pending.Sign() > 0 && (units.Sign() == 0 || day.Price != nil && pending.Cmp(new(big.Rat).Mul(units, day.Price)) > 0) {
				stopped = "insufficient_benchmark_balance"
				point.State = "stopped"
				point.Reason = stopped
				out.Points = append(out.Points, point)
				continue
			}
			if day.CanTrade {
				if pending.Sign() > 0 {
					units.Sub(units, new(big.Rat).Quo(pending, day.Price))
					pending.SetInt64(0)
				}
				if cash.Sign() > 0 {
					units.Add(units, new(big.Rat).Quo(cash, day.Price))
					cash.SetInt64(0)
				}
			}
		}
		point.Units = new(big.Rat).Set(units)
		point.Cash = new(big.Rat).Set(cash)
		point.PendingWithdrawal = new(big.Rat).Set(pending)
		point.State = "ready"
		if day.Price != nil {
			point.Value = new(big.Rat).Sub(new(big.Rat).Add(new(big.Rat).Mul(units, day.Price), cash), pending)
		} else if units.Sign() == 0 && pending.Sign() == 0 {
			point.Value = new(big.Rat).Set(cash)
		} else {
			point.State = "gap"
			point.Reason = "benchmark_price_unavailable"
		}
		if pending.Sign() > 0 {
			point.State = "pending"
			point.Reason = "benchmark_withdrawal_pending"
			// Cannot present an unexecuted withdrawal as a completed comparison day.
			point.Value = nil
		}
		out.Points = append(out.Points, point)
	}
	out.Reason = stopped
	return out, nil
}
