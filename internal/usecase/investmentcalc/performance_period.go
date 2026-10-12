package investmentcalc

import (
	"fmt"
	"math/big"
	"time"
)

// PeriodPerformanceInput starts with NAV(s-1), followed by every calendar day
// in [s,e]. Baseline-day cash flows are outside the period and are ignored.
// FlowCoverage must confirm the journal is complete, independently of NAV.
// ResetDates mark opening balances/history changes within the measured period.
type PeriodPerformanceInput struct {
	Days         []PerformanceDay
	FlowCoverage bool
	ResetDates   map[string]bool
}

type PeriodPerformance struct {
	From, To                 string
	TWR                      TWRResult
	XIRR                     XIRRResult
	Profit, NetContributions *big.Rat
	Reason                   string // reason profit is unavailable
	FlowReason               string // independent completeness of net contributions
}

// CalculatePeriod assembles investor cash flows without treating purchases,
// dividends or fees as new capital. Intermediate NAV gaps break TWR but do not
// invalidate XIRR when boundaries and the entire external-flow history are known.
func CalculatePeriod(input PeriodPerformanceInput) (PeriodPerformance, error) {
	out := PeriodPerformance{XIRR: XIRRResult{Annualized: true}}
	if len(input.Days) < 2 {
		out.Reason = "insufficient_history"
		out.FlowReason = out.Reason
		out.TWR.Reason = out.Reason
		out.XIRR.Reason = out.Reason
		return out, nil
	}
	days := append([]PerformanceDay{}, input.Days...)
	out.From = days[1].Date
	out.To = days[len(days)-1].Date
	dates := map[string]bool{}
	reason := ""
	var previous time.Time
	for i, day := range days {
		date, err := time.Parse(time.DateOnly, day.Date)
		if err != nil || date.Format(time.DateOnly) != day.Date || i > 0 && !date.After(previous) {
			return out, fmt.Errorf("ordered unique reporting dates required")
		}
		if i > 0 && date.Unix()-previous.Unix() != 86400 {
			reason = "calendar_gap"
		}
		previous = date
		dates[day.Date] = true
		if i == 0 {
			days[i].ExternalFlow = new(big.Rat)
			continue
		}
		if !input.FlowCoverage {
			days[i].ExternalFlow = nil
		}
		if input.ResetDates[day.Date] {
			days[i].Complete = false
		}
	}
	for date, reset := range input.ResetDates {
		if reset && !dates[date] {
			return out, fmt.Errorf("reset outside reporting range")
		}
	}
	var err error
	out.TWR, err = DailyTWR(days)
	if err != nil {
		return out, err
	}
	for i := range out.TWR.Points {
		if i > 0 && input.ResetDates[out.TWR.Points[i].Date] {
			out.TWR.Points[i].Reason = "history_reset"
		}
	}
	fail := func(code string) (PeriodPerformance, error) {
		out.Reason = code
		out.XIRR.Reason = code
		return out, nil
	}
	if reason != "" {
		out.FlowReason = reason
		return fail(reason)
	}
	if !input.FlowCoverage {
		out.FlowReason = "external_flow_unknown"
		return fail(out.FlowReason)
	}
	net := new(big.Rat)
	for _, day := range days[1:] {
		if day.ExternalFlow == nil {
			out.FlowReason = "external_flow_unknown"
			return fail(out.FlowReason)
		}
		net.Add(net, day.ExternalFlow)
	}
	out.NetContributions = net
	for i, day := range days {
		if i > 0 && input.ResetDates[day.Date] {
			return fail("history_reset")
		}
	}
	first, last := days[0], days[len(days)-1]
	if !first.Complete || first.NAV == nil {
		return fail("initial_nav_unknown")
	}
	if !last.Complete || last.NAV == nil {
		return fail("terminal_nav_unknown")
	}
	for _, day := range days {
		if day.NAV != nil && day.NAV.Sign() < 0 {
			return fail("negative_nav")
		}
	}
	investor := []DatedCashFlow{{Date: first.Date, Amount: new(big.Rat).Neg(first.NAV)}}
	for _, day := range days[1:] {
		investor = append(investor, DatedCashFlow{Date: day.Date, Amount: new(big.Rat).Neg(day.ExternalFlow)})
	}
	investor = append(investor, DatedCashFlow{Date: last.Date, Amount: new(big.Rat).Set(last.NAV)})
	out.NetContributions = net
	out.Profit = new(big.Rat).Sub(new(big.Rat).Sub(last.NAV, first.NAV), net)
	out.XIRR = XIRR(investor)
	return out, nil
}
