package investmentcalc

import (
	"math"
	"math/big"
	"testing"
	"time"
)

func performanceNumber(s string) *big.Rat {
	v, ok := new(big.Rat).SetString(s)
	if !ok {
		panic(s)
	}
	return v
}
func performanceDay(date, nav, flow string) PerformanceDay {
	return PerformanceDay{Date: date, NAV: performanceNumber(nav), ExternalFlow: performanceNumber(flow), Complete: true}
}
func TestDailyTWRExternalFlowsAndFees(t *testing.T) {
	days := []PerformanceDay{performanceDay("2024-01-01", "1000", "0"), performanceDay("2024-01-02", "1520", "500"), performanceDay("2024-01-03", "1020", "-500")}
	result, e := DailyTWR(days)
	if e != nil || result.Return.RatString() != "1/50" || result.Points[1].DailyReturn.RatString() != "1/50" || result.Points[2].DailyReturn.Sign() != 0 {
		t.Fatal(result, e)
	}
	if days[1].NAV.RatString() != "1520" || days[1].ExternalFlow.RatString() != "500" {
		t.Fatal("caller values mutated")
	}
	// A commission is already in NAV, never an external withdrawal to be removed.
	days[1].NAV = performanceNumber("1518")
	days = days[:2]
	result, e = DailyTWR(days)
	if e != nil || result.Return.RatString() != "9/500" {
		t.Fatal(result, e)
	}
}
func TestDailyTWRBreaksAndFunding(t *testing.T) {
	days := []PerformanceDay{performanceDay("2024-01-01", "0", "0"), performanceDay("2024-01-02", "1000", "1000"), performanceDay("2024-01-03", "1100", "0"), performanceDay("2024-01-04", "0", "-1100"), performanceDay("2024-01-05", "500", "500"), performanceDay("2024-01-06", "550", "0")}
	result, e := DailyTWR(days)
	if e != nil || result.Return != nil || result.Reason != "discontinuous_history" || result.Points[1].State != "baseline" || result.Points[3].State != "gap" || result.Points[5].Segment != 2 || result.Points[5].SegmentReturn.RatString() != "1/10" {
		t.Fatal(result, e)
	}
	days = []PerformanceDay{performanceDay("2024-01-01", "1000", "0"), performanceDay("2024-01-03", "1100", "0"), performanceDay("2024-01-04", "1210", "0")}
	result, e = DailyTWR(days)
	if e != nil || result.Return != nil || result.Points[1].State != "baseline" || result.Points[2].DailyReturn.RatString() != "1/10" {
		t.Fatal(result, e)
	}
	for _, change := range []func(*PerformanceDay){func(d *PerformanceDay) { d.Complete = false }, func(d *PerformanceDay) { d.NAV = nil }, func(d *PerformanceDay) { d.ExternalFlow = nil }, func(d *PerformanceDay) { d.NAV = performanceNumber("-1") }} {
		day := performanceDay("2024-01-02", "1100", "0")
		change(&day)
		r, e := DailyTWR([]PerformanceDay{days[0], day})
		if e != nil || r.Return != nil || r.Points[1].State != "gap" {
			t.Fatal(r, e)
		}
	}
	if _, e = DailyTWR([]PerformanceDay{days[0], days[0]}); e == nil {
		t.Fatal("duplicate day accepted")
	}
}
func annualFlows(amounts ...string) []DatedCashFlow {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	out := []DatedCashFlow{}
	for i, amount := range amounts {
		out = append(out, DatedCashFlow{Date: start.AddDate(0, 0, i*365).Format(time.DateOnly), Amount: performanceNumber(amount)})
	}
	return out
}
func TestXIRRAnnualAndShortPeriod(t *testing.T) {
	for _, tc := range []struct {
		flows []DatedCashFlow
		want  float64
	}{
		{annualFlows("-1000", "1100"), 0.1},
		{annualFlows("-1000", "900"), -0.1},
		{annualFlows("-1000", "1000"), 0},
		{[]DatedCashFlow{{"2024-01-01", performanceNumber("-100")}, {"2024-01-02", performanceNumber("101")}}, math.Pow(1.01, 365) - 1},
	} {
		r := XIRR(tc.flows)
		if r.Rate == nil || math.Abs(*r.Rate-tc.want) > 1e-9*math.Max(1, math.Abs(tc.want)) || !r.Annualized {
			t.Fatal(r, tc.want)
		}
	}
}
func TestXIRRMultipleRootsAndUncertainInputs(t *testing.T) {
	for _, tc := range []struct {
		flows  []DatedCashFlow
		reason string
	}{
		{annualFlows("-100", "230", "-132"), "multiple_roots"},
		{annualFlows("-100", "360", "-431", "171.6"), "multiple_roots"},
		{annualFlows("-1000000000000000000", "1000000000000000001"), "numerical_precision"},
		{annualFlows("-100000000000000000000", "0.00000000000000000001"), "rate_precision_limit"},
		{annualFlows("-100", "200", "-100"), "non_simple_root"},
		{annualFlows("-100", "100", "-100"), "no_root"},
		{annualFlows("100", "200"), "no_root"},
		{annualFlows("-100", "0"), "insufficient_dates"},
		{[]DatedCashFlow{{"2024-01-01", nil}}, "cash_flow_unknown"},
		{[]DatedCashFlow{{"bad-date", performanceNumber("100")}}, "invalid_date"},
	} {
		r := XIRR(tc.flows)
		if r.Rate != nil || r.Reason != tc.reason {
			t.Fatal(tc.reason, r)
		}
	}
	// Nonconventional signs do not necessarily imply multiple roots.
	flows := annualFlows("-100", "50", "-20", "100")
	r := XIRR(flows)
	if r.Rate == nil {
		t.Fatal(r)
	}
	npv := 0.0
	for i, f := range flows {
		amount, _ := f.Amount.Float64()
		npv += amount / math.Pow(1+*r.Rate, float64(i))
	}
	if math.Abs(npv) > 1e-8 {
		t.Fatal("nonconventional residual", r, npv)
	}
	// Same-day movements combine exactly before signs or numerical conversion.
	flows = append(annualFlows("-1000", "1100"), DatedCashFlow{Date: "2020-01-01", Amount: performanceNumber("-50")}, DatedCashFlow{Date: "2020-01-01", Amount: performanceNumber("50")})
	r = XIRR(flows)
	if r.Rate == nil || math.Abs(*r.Rate-.1) > 1e-10 {
		t.Fatal(r)
	}
}

func TestXIRRLongHistoryAndComplexityBound(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	conventional := []DatedCashFlow{}
	for i := 0; i < 300; i++ {
		amount := "-100"
		if i == 299 {
			amount = "31000"
		}
		conventional = append(conventional, DatedCashFlow{start.AddDate(0, 0, i).Format(time.DateOnly), performanceNumber(amount)})
	}
	result := XIRR(conventional)
	if result.Rate == nil || *result.Rate <= 0 {
		t.Fatal("conventional history unnecessarily limited", result)
	}
	for i := range conventional {
		amount := "-100"
		if i%2 == 1 {
			amount = "100"
		}
		conventional[i].Amount = performanceNumber(amount)
	}
	result = XIRR(conventional)
	if result.Rate != nil || result.Reason != "complexity_limit" {
		t.Fatal(result)
	}
	// time.Duration cannot span this interval; calendar offsets must not saturate.
	result = XIRR([]DatedCashFlow{{"1900-01-01", performanceNumber("-100")}, {"2200-01-01", performanceNumber("110")}})
	from, _ := time.Parse(time.DateOnly, "1900-01-01")
	to, _ := time.Parse(time.DateOnly, "2200-01-01")
	days := int((to.Unix() - from.Unix()) / 86400)
	expected := math.Expm1(math.Log(1.1) * 365 / float64(days))
	if result.Rate == nil || result.Days != days || math.Abs(*result.Rate-expected) > 1e-10 {
		t.Fatal(result, expected)
	}
}
