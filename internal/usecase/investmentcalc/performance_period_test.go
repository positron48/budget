package investmentcalc

import (
	"math"
	"math/big"
	"testing"
	"time"
)

func TestCalculatePeriodContributionsAndBaseline(t *testing.T) {
	days := []PerformanceDay{performanceDay("2026-01-01", "1000", "900"), performanceDay("2026-01-02", "1520", "500")}
	got, err := CalculatePeriod(PeriodPerformanceInput{Days: days, FlowCoverage: true})
	if err != nil || got.Profit.Cmp(big.NewRat(20, 1)) != 0 || got.NetContributions.Cmp(big.NewRat(500, 1)) != 0 || got.TWR.Return.Cmp(big.NewRat(1, 50)) != 0 {
		t.Fatalf("C05: %+v %v", got, err)
	}
	// Baseline flows are not paid twice; XIRR sees -1000 at baseline and +1020 next day.
	expected := math.Pow(1.02, 365) - 1
	if got.XIRR.Rate == nil || math.Abs(*got.XIRR.Rate-expected) > expected*1e-9 {
		t.Fatalf("investor signs: %+v", got.XIRR)
	}
	if days[0].ExternalFlow.Cmp(big.NewRat(900, 1)) != 0 {
		t.Fatal("mutated baseline")
	}
}

func TestCalculatePeriodIntermediateNAVGap(t *testing.T) {
	days := []PerformanceDay{}
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= 365; i++ {
		days = append(days, performanceDay(start.AddDate(0, 0, i).Format(time.DateOnly), "1000", "0"))
	}
	days[365].NAV = big.NewRat(1100, 1)
	days[100].NAV = nil
	days[100].Complete = false
	got, err := CalculatePeriod(PeriodPerformanceInput{Days: days, FlowCoverage: true})
	if err != nil || got.XIRR.Rate == nil || math.Abs(*got.XIRR.Rate-.1) > 1e-10 || got.TWR.Return != nil || got.Profit.Cmp(big.NewRat(100, 1)) != 0 {
		t.Fatalf("C18 independent evidence: %+v %v", got, err)
	}
}

func TestCalculatePeriodMissingEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		change       func(*PeriodPerformanceInput)
	}{
		{"coverage", "external_flow_unknown", func(q *PeriodPerformanceInput) { q.FlowCoverage = false }},
		{"flow", "external_flow_unknown", func(q *PeriodPerformanceInput) { q.Days[1].ExternalFlow = nil }},
		{"initial", "initial_nav_unknown", func(q *PeriodPerformanceInput) { q.Days[0].Complete = false }},
		{"terminal", "terminal_nav_unknown", func(q *PeriodPerformanceInput) { q.Days[1].Complete = false }},
		{"negative", "negative_nav", func(q *PeriodPerformanceInput) { q.Days[1].NAV = big.NewRat(-1, 1) }},
		{"opening", "history_reset", func(q *PeriodPerformanceInput) { q.ResetDates = map[string]bool{"2026-01-02": true} }},
		{"gap", "calendar_gap", func(q *PeriodPerformanceInput) { q.Days[1].Date = "2026-01-03" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := PeriodPerformanceInput{Days: []PerformanceDay{performanceDay("2026-01-01", "1000", "0"), performanceDay("2026-01-02", "1100", "0")}, FlowCoverage: true}
			tc.change(&q)
			got, err := CalculatePeriod(q)
			if err != nil || got.XIRR.Rate != nil || got.Profit != nil || got.Reason != tc.reason || got.TWR.Return != nil {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestCalculatePeriodFullWithdrawal(t *testing.T) {
	got, err := CalculatePeriod(PeriodPerformanceInput{Days: []PerformanceDay{performanceDay("2026-01-01", "1000", "0"), performanceDay("2026-01-02", "0", "-1100")}, FlowCoverage: true})
	if err != nil || got.Profit.Cmp(big.NewRat(100, 1)) != 0 || got.TWR.Return != nil || got.XIRR.Rate == nil {
		t.Fatalf("full withdrawal: %+v %v", got, err)
	}
}
