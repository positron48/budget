package investmentcalc

import (
	"fmt"
	"math/big"
	"time"
)

// ProfitPoint's cumulative result keeps the original period baseline. A missing
// interior NAV breaks the line but does not erase a later known cumulative result.
type ProfitPoint struct {
	Date                          string
	Daily, Cumulative             *big.Rat
	DailyReason, CumulativeReason string
}
type ProfitBucket struct {
	From, To, Period, Reason string
	Profit, NetContributions *big.Rat
}
type ProfitTimeline struct {
	Points        []ProfitPoint
	Months, Years []ProfitBucket
}

func CalculateProfitTimeline(input PeriodPerformanceInput) (ProfitTimeline, error) {
	out := ProfitTimeline{}
	if len(input.Days) < 2 {
		return out, nil
	}
	// Share validation and period conventions with the headline performance result.
	if _, e := CalculatePeriod(input); e != nil {
		return out, e
	}
	days := input.Days
	prefix := make([]*big.Rat, len(days))
	badFlow := make([]int, len(days))
	resets := make([]int, len(days))
	negative := make([]int, len(days))
	gaps := make([]int, len(days))
	prefix[0] = new(big.Rat)
	if days[0].NAV != nil && days[0].NAV.Sign() < 0 {
		negative[0] = 1
	}
	for i := 1; i < len(days); i++ {
		prefix[i] = new(big.Rat).Set(prefix[i-1])
		badFlow[i] = badFlow[i-1]
		resets[i] = resets[i-1]
		negative[i] = negative[i-1]
		gaps[i] = gaps[i-1]
		if !input.FlowCoverage || days[i].ExternalFlow == nil {
			badFlow[i]++
		} else {
			prefix[i].Add(prefix[i], days[i].ExternalFlow)
		}
		if input.ResetDates[days[i].Date] {
			resets[i]++
		}
		if days[i].NAV != nil && days[i].NAV.Sign() < 0 {
			negative[i]++
		}
		a, _ := time.Parse(time.DateOnly, days[i-1].Date)
		b, _ := time.Parse(time.DateOnly, days[i].Date)
		if b.Sub(a) != 24*time.Hour {
			gaps[i]++
		}
	}
	period := func(start, end int) (*big.Rat, *big.Rat, string) {
		net := new(big.Rat).Sub(prefix[end], prefix[start])
		if gaps[end] != gaps[start] {
			return nil, nil, "calendar_gap"
		}
		if badFlow[end] != badFlow[start] {
			return nil, nil, "external_flow_unknown"
		}
		if resets[end] != resets[start] {
			return nil, net, "history_reset"
		}
		if !days[start].Complete || days[start].NAV == nil {
			return nil, net, "initial_nav_unknown"
		}
		if !days[end].Complete || days[end].NAV == nil {
			return nil, net, "terminal_nav_unknown"
		}
		baseNegative := 0
		if days[start].NAV.Sign() < 0 {
			baseNegative = 1
		}
		if negative[end]-negative[start]+baseNegative > 0 {
			return nil, net, "negative_nav"
		}
		return new(big.Rat).Sub(new(big.Rat).Sub(days[end].NAV, days[start].NAV), net), net, ""
	}
	base := ProfitPoint{Date: days[0].Date}
	if days[0].Complete && days[0].NAV != nil && days[0].NAV.Sign() >= 0 {
		base.Cumulative = new(big.Rat)
	} else {
		base.CumulativeReason = "initial_nav_unknown"
	}
	out.Points = append(out.Points, base)
	for i := 1; i < len(days); i++ {
		p := ProfitPoint{Date: days[i].Date}
		p.Daily, _, p.DailyReason = period(i-1, i)
		p.Cumulative, _, p.CumulativeReason = period(0, i)
		out.Points = append(out.Points, p)
	}
	buckets := func(width int) []ProfitBucket {
		values := []ProfitBucket{}
		start := 1
		for start < len(days) {
			end := start
			key := days[start].Date[:width]
			for end+1 < len(days) && days[end+1].Date[:width] == key {
				end++
			}
			p := ProfitBucket{From: days[start].Date, To: days[end].Date, Period: key}
			p.Profit, p.NetContributions, p.Reason = period(start-1, end)
			values = append(values, p)
			start = end + 1
		}
		return values
	}
	out.Months = buckets(7)
	out.Years = buckets(4)
	return out, nil
}

// ProfitEvent is a journal fact before conversion. Amount nil means unknown,
// not zero. Trade fees included in FIFO are informational, never subtracted twice.
type ProfitEvent struct {
	OperationID, LedgerID, InstrumentKey, Currency, Kind, Reason string
	At                                                           time.Time
	Amount                                                       *big.Rat
}
type ProfitComponent struct {
	Kind, Reason  string
	Known         *big.Rat
	Complete      bool
	Informational bool
}
type ProfitAttribution struct {
	Components []ProfitComponent
	Complete   bool
	Reason     string
}

// ReconcileProfit never creates a second profit total. It exposes the remaining
// FX/residual only when all additive components and the NAV/flow total are known.
func ReconcileProfit(total *big.Rat, components []ProfitComponent) (ProfitAttribution, error) {
	out := ProfitAttribution{Components: components, Complete: total != nil}
	sum := new(big.Rat)
	seen := map[string]bool{}
	for _, c := range components {
		if c.Kind == "" || c.Kind == "fx_residual" || seen[c.Kind] {
			return out, fmt.Errorf("unique profit components required")
		}
		seen[c.Kind] = true
		if c.Informational {
			continue
		}
		if !c.Complete || c.Known == nil {
			out.Complete = false
		} else {
			sum.Add(sum, c.Known)
		}
	}
	residual := ProfitComponent{Kind: "fx_residual", Complete: out.Complete}
	if out.Complete {
		residual.Known = new(big.Rat).Sub(total, sum)
	} else {
		residual.Reason = "attribution_incomplete"
		out.Reason = residual.Reason
	}
	out.Components = append(out.Components, residual)
	return out, nil
}
