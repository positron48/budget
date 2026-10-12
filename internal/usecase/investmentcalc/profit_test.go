package investmentcalc

import (
	"math/big"
	"testing"
)

func TestProfitTimelineBoundariesGapsAndBuckets(t *testing.T) {
	day := func(date string, nav, flow int64) PerformanceDay {
		return PerformanceDay{Date: date, NAV: big.NewRat(nav, 1), ExternalFlow: big.NewRat(flow, 1), Complete: true}
	}
	input := PeriodPerformanceInput{FlowCoverage: true, Days: []PerformanceDay{day("2024-01-30", 100, 999), day("2024-01-31", 152, 50), day("2024-02-01", 163, 10), day("2024-02-02", 160, 0)}}
	value, e := CalculateProfitTimeline(input)
	headline, _ := CalculatePeriod(input)
	if e != nil || value.Points[0].Cumulative.Sign() != 0 || value.Points[1].Daily.RatString() != "2" || value.Points[2].Cumulative.RatString() != "3" || value.Points[3].Cumulative.Cmp(headline.Profit) != 0 || value.Months[0].Profit.RatString() != "2" || value.Months[1].Profit.RatString() != "-2" || value.Years[0].Profit.RatString() != "0" {
		t.Fatal(value, e)
	}
	input.Days[2].Complete = false
	input.Days[2].NAV = nil
	value, e = CalculateProfitTimeline(input)
	if e != nil || value.Points[2].Cumulative != nil || value.Points[3].Daily != nil || value.Points[3].Cumulative.RatString() != "0" {
		t.Fatal("cumulative should recover against original baseline", value, e)
	}
	input.Days[2].ExternalFlow = nil
	value, _ = CalculateProfitTimeline(input)
	if value.Points[3].Cumulative != nil || value.Points[3].CumulativeReason != "external_flow_unknown" {
		t.Fatal(value)
	}
	input.Days[2] = day("2024-02-01", 163, 10)
	input.ResetDates = map[string]bool{"2024-02-01": true}
	value, _ = CalculateProfitTimeline(input)
	if value.Points[3].Cumulative != nil || value.Points[3].CumulativeReason != "history_reset" || value.Points[3].Daily.RatString() != "-3" {
		t.Fatal(value)
	}
}
func TestProfitReconciliationUnknownsAndFees(t *testing.T) {
	components := []ProfitComponent{{Kind: "realized_clean", Known: big.NewRat(18, 1), Complete: true}, {Kind: "unrealized_clean_change", Known: big.NewRat(19, 1), Complete: true}, {Kind: "earned_income", Known: big.NewRat(10, 1), Complete: true}, {Kind: "taxes", Known: big.NewRat(-1, 1), Complete: true}, {Kind: "trade_fees_in_cost", Known: big.NewRat(3, 1), Complete: true, Informational: true}}
	out, e := ReconcileProfit(big.NewRat(46, 1), components)
	if e != nil || !out.Complete || out.Components[5].Known.Sign() != 0 {
		t.Fatal(out, e)
	}
	components[1].Complete = false
	components[1].Known = nil
	out, e = ReconcileProfit(big.NewRat(46, 1), components)
	if e != nil || out.Complete || out.Components[5].Known != nil {
		t.Fatal("unknown cannot be invented as FX residual", out, e)
	}
}
