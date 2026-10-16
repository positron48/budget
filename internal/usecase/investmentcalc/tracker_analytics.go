package investmentcalc

import (
	"fmt"
	"math/big"
	"time"
)

type StrategyAnalyticsPoint struct {
	Date, Reason, FlowReason                                  string
	Value, Flow, Contributions, Invested, Profit, DailyProfit *big.Rat
}
type StrategyAnalyticsPeriod struct {
	Key, From, To, Reason string
	Profit                *big.Rat
}
type StrategyAnalytics struct {
	Points        []StrategyAnalyticsPoint
	Months, Years []StrategyAnalyticsPeriod
}

// Cashless strategy flows are supplied by the tracker: purchases and fees add
// capital, sales and received payouts remove it. Payouts are not added again.
// Missing intermediate valuations break daily profit, but boundary profit can
// recover when every flow and both valuation boundaries remain known.
func CalculateStrategyAnalytics(days []PerformanceDay, resets map[string]bool) (StrategyAnalytics, error) {
	out := StrategyAnalytics{}
	if len(days) < 2 {
		return out, fmt.Errorf("baseline and measured days required")
	}
	for i, d := range days {
		at, e := time.Parse(time.DateOnly, d.Date)
		if e != nil || at.Format(time.DateOnly) != d.Date || i > 0 && at.Sub(mustAnalyticsDate(days[i-1].Date)) != 24*time.Hour || d.NAV != nil && d.NAV.Sign() < 0 {
			return out, fmt.Errorf("consecutive dates and nonnegative values required")
		}
	}
	boundary := func(first, last int) (*big.Rat, string) {
		if !days[first].Complete || days[first].NAV == nil || !days[last].Complete || days[last].NAV == nil {
			return nil, "valuation_missing"
		}
		flows := new(big.Rat)
		for i := first + 1; i <= last; i++ {
			if resets[days[i].Date] {
				return nil, "history_reset"
			}
			if days[i].ExternalFlow == nil {
				return nil, "strategy_flow_missing"
			}
			flows.Add(flows, days[i].ExternalFlow)
		}
		return new(big.Rat).Sub(new(big.Rat).Sub(days[last].NAV, days[first].NAV), flows), ""
	}
	total := new(big.Rat)
	flowReason := ""
	for i, d := range days {
		p := StrategyAnalyticsPoint{Date: d.Date}
		if d.Complete && d.NAV != nil {
			p.Value = new(big.Rat).Set(d.NAV)
		}
		if i == 0 {
			p.Flow = new(big.Rat)
		} else {
			if resets[d.Date] {
				flowReason = "history_reset"
			}
			if d.ExternalFlow == nil {
				if flowReason == "" {
					flowReason = "strategy_flow_missing"
				}
			} else {
				p.Flow = new(big.Rat).Set(d.ExternalFlow)
				total.Add(total, d.ExternalFlow)
			}
			p.DailyProfit, _ = boundary(i-1, i)
		}
		p.FlowReason = flowReason
		if flowReason == "" {
			p.Contributions = new(big.Rat).Set(total)
			if days[0].Complete && days[0].NAV != nil {
				p.Invested = new(big.Rat).Add(days[0].NAV, total)
				if p.Value != nil {
					p.Profit = new(big.Rat).Sub(p.Value, p.Invested)
				}
			}
		}
		p.Reason = flowReason
		if p.Profit == nil && p.Reason == "" {
			p.Reason = "valuation_missing"
		}
		out.Points = append(out.Points, p)
	}
	periods := func(width int) []StrategyAnalyticsPeriod {
		result := []StrategyAnalyticsPeriod{}
		for first := 1; first < len(days); {
			last := first
			key := days[first].Date[:width]
			for last+1 < len(days) && days[last+1].Date[:width] == key {
				last++
			}
			value, reason := boundary(first-1, last)
			result = append(result, StrategyAnalyticsPeriod{Key: key, From: days[first].Date, To: days[last].Date, Profit: value, Reason: reason})
			first = last + 1
		}
		return result
	}
	out.Months, out.Years = periods(7), periods(4)
	return out, nil
}
func mustAnalyticsDate(s string) time.Time { t, _ := time.Parse(time.DateOnly, s); return t }

// VirtualStrategyValues is a retrospective comparison, not an execution model.
// Fractional units rebalance at each eligible day's display-currency valuation,
// including carried closes. There are no fees or invented dividends. Unknown
// flows or resets stop the simulation. During a price gap, net flows wait
// as cash or a withdrawal liability and execute at the next eligible close;
// earlier missing valuations stay missing.
func VirtualStrategyValues(initial *big.Rat, days []BenchmarkDay) (BenchmarkResult, error) {
	out := BenchmarkResult{}
	if initial == nil || initial.Sign() < 0 || len(days) == 0 {
		return out, fmt.Errorf("known nonnegative baseline required")
	}
	// Rounding the modeled money to 24 decimals bounds denominator growth over
	// thousands of distinct price/FX ratios. Real portfolio money stays exact.
	value := new(big.Rat).Set(initial)
	var previousPrice *big.Rat
	pending := new(big.Rat)
	stopped := ""
	for i, d := range days {
		if _, e := time.Parse(time.DateOnly, d.Date); e != nil || i > 0 && mustAnalyticsDate(d.Date).Sub(mustAnalyticsDate(days[i-1].Date)) != 24*time.Hour || d.Price != nil && d.Price.Sign() <= 0 {
			return out, fmt.Errorf("consecutive dates and positive prices required")
		}
		p := BenchmarkPoint{Date: d.Date, State: "ready"}
		if i == 0 {
			if d.Price == nil {
				stopped = "initial_benchmark_price_unavailable"
			} else {
				previousPrice = new(big.Rat).Set(d.Price)
			}
		} else if stopped == "" {
			switch {
			case d.HistoryReset:
				stopped = "history_reset"
			case d.ExternalFlow == nil:
				stopped = "strategy_flow_missing"
			case d.Price == nil:
				pending.Add(pending, d.ExternalFlow)
				if value.Sign() == 0 && pending.Sign() < 0 {
					stopped = "insufficient_benchmark_balance"
				}
			case d.Price != nil:
				next := new(big.Rat).Add(new(big.Rat).Mul(value, new(big.Rat).Quo(d.Price, previousPrice)), new(big.Rat).Add(pending, d.ExternalFlow))
				pending.SetInt64(0)
				if next.Sign() < 0 {
					stopped = "insufficient_benchmark_balance"
				} else {
					value, _ = new(big.Rat).SetString(next.FloatString(24))
					previousPrice = new(big.Rat).Set(d.Price)
				}
			}
		}
		if stopped != "" {
			p.State, p.Reason = "stopped", stopped
		} else if d.Price != nil {
			p.Value = new(big.Rat).Set(value)
			p.Units = new(big.Rat).Quo(value, d.Price)
		} else if value.Sign() == 0 && pending.Sign() >= 0 {
			p.Value = new(big.Rat).Set(pending)
		} else {
			p.State, p.Reason = "gap", "benchmark_price_unavailable"
		}
		out.Points = append(out.Points, p)
	}
	out.Reason = stopped
	return out, nil
}
