package investmentcalc

import (
	"math/big"
	"testing"
	"time"
)

func analyticsDay(date string, value, flow int64) PerformanceDay {
	return PerformanceDay{Date: date, NAV: big.NewRat(value, 1), ExternalFlow: big.NewRat(flow, 1), Complete: true}
}
func TestStrategyAnalyticsPayoutsBoundariesAndPeriods(t *testing.T) {
	days := []PerformanceDay{analyticsDay("2024-12-30", 100, 999), analyticsDay("2024-12-31", 110, -5), analyticsDay("2025-01-01", 150, 40), analyticsDay("2025-01-02", 140, -20)}
	a, e := CalculateStrategyAnalytics(days, nil)
	if e != nil {
		t.Fatal(e)
	}
	for i, want := range []int64{0, 15, 15, 25} {
		p := a.Points[i]
		if p.Profit.Cmp(big.NewRat(want, 1)) != 0 {
			t.Fatal(i, p)
		}
	}
	if a.Points[3].Contributions.Cmp(big.NewRat(15, 1)) != 0 || a.Points[3].Invested.Cmp(big.NewRat(115, 1)) != 0 {
		t.Fatal(a.Points[3])
	}
	if len(a.Months) != 2 || a.Months[0].Profit.Cmp(big.NewRat(15, 1)) != 0 || a.Months[1].Profit.Cmp(big.NewRat(10, 1)) != 0 || len(a.Years) != 2 {
		t.Fatal(a)
	}
	if a.Points[1].DailyProfit.Cmp(big.NewRat(15, 1)) != 0 {
		t.Fatal("payout omitted or doubled")
	}
}
func TestStrategyAnalyticsMissingValuesRecoverButUnknownFlowsDoNot(t *testing.T) {
	days := []PerformanceDay{analyticsDay("2024-01-30", 100, 0), analyticsDay("2024-01-31", 120, 10), analyticsDay("2024-02-01", 130, 0), analyticsDay("2024-02-02", 140, 0)}
	days[1].Complete = false
	a, _ := CalculateStrategyAnalytics(days, nil)
	if a.Points[1].Profit != nil || a.Points[2].DailyProfit != nil || a.Points[2].Profit.Cmp(big.NewRat(20, 1)) != 0 || a.Points[3].DailyProfit.Cmp(big.NewRat(10, 1)) != 0 || a.Months[1].Profit != nil {
		t.Fatal(a)
	}
	days[1].ExternalFlow = nil
	a, _ = CalculateStrategyAnalytics(days, nil)
	if a.Points[3].Profit != nil || a.Points[3].Contributions != nil || a.Points[3].FlowReason != "strategy_flow_missing" {
		t.Fatal(a.Points[3])
	}
	days[1].ExternalFlow = big.NewRat(10, 1)
	days[1].Complete = true
	a, _ = CalculateStrategyAnalytics(days, map[string]bool{"2024-01-31": true})
	if a.Points[3].Profit != nil || a.Months[1].Profit == nil || a.Months[1].Profit.Cmp(big.NewRat(20, 1)) != 0 {
		t.Fatal(a)
	}
}
func TestStrategyAnalyticsRequiresConsecutiveDays(t *testing.T) {
	if _, e := CalculateStrategyAnalytics([]PerformanceDay{analyticsDay("2024-01-01", 100, 0), analyticsDay("2024-01-03", 110, 0)}, nil); e == nil {
		t.Fatal("calendar gap accepted")
	}
}
func TestVirtualStrategyValuesApplySameFlowsWithoutPayoutDoubleCount(t *testing.T) {
	days := []BenchmarkDay{{Date: "2024-01-01", Price: big.NewRat(100, 1)}, {Date: "2024-01-02", Price: big.NewRat(110, 1), ExternalFlow: big.NewRat(55, 1)}, {Date: "2024-01-03", Price: big.NewRat(120, 1), ExternalFlow: big.NewRat(-30, 1)}}
	a, e := VirtualStrategyValues(big.NewRat(100, 1), days)
	if e != nil || a.Points[1].Value.Cmp(big.NewRat(165, 1)) != 0 || a.Points[2].Value.Cmp(big.NewRat(150, 1)) != 0 {
		t.Fatal(a, e)
	}
	days[1].Price = nil
	days[1].ExternalFlow = big.NewRat(0, 1)
	a, _ = VirtualStrategyValues(big.NewRat(100, 1), days)
	if a.Points[1].Value != nil || a.Points[2].Value.Cmp(big.NewRat(90, 1)) != 0 {
		t.Fatal(a)
	}
	days[1].ExternalFlow = big.NewRat(1, 1)
	a, _ = VirtualStrategyValues(big.NewRat(100, 1), days)
	if a.Points[1].Value != nil || a.Points[2].Value.Cmp(big.NewRat(91, 1)) != 0 || a.Reason != "" {
		t.Fatal(a)
	}
}
func TestVirtualStrategyCannotBorrowOrGuessFlow(t *testing.T) {
	for _, flow := range []*big.Rat{nil, big.NewRat(-101, 1)} {
		a, e := VirtualStrategyValues(big.NewRat(100, 1), []BenchmarkDay{{Date: "2024-01-01", Price: big.NewRat(100, 1)}, {Date: "2024-01-02", Price: big.NewRat(100, 1), ExternalFlow: flow}})
		if e != nil || a.Points[1].Value != nil || a.Reason == "" {
			t.Fatal(a, e)
		}
	}
}

func TestVirtualStrategyTenYearsHaveBoundedMoneyPrecision(t *testing.T) {
	first := time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC)
	days := []BenchmarkDay{}
	for i := 0; i < 3661; i++ {
		days = append(days, BenchmarkDay{Date: first.AddDate(0, 0, i).Format(time.DateOnly), Price: big.NewRat(int64(101+i), 1), ExternalFlow: big.NewRat(1, 1)})
	}
	out, e := VirtualStrategyValues(big.NewRat(100, 1), days)
	if e != nil || len(out.Points) != 3661 {
		t.Fatal(e)
	}
	for _, p := range out.Points {
		if p.Value == nil || p.Value.Denom().BitLen() > 80 {
			t.Fatal("virtual denominator grew with history")
		}
	}
	// Compare a shorter series against an entirely exact fractional-unit model.
	exactUnits := big.NewRat(100, 101)
	for i := 1; i < 100; i++ {
		exactUnits.Add(exactUnits, new(big.Rat).Inv(days[i].Price))
	}
	exactValue := new(big.Rat).Mul(exactUnits, days[99].Price)
	errorValue := new(big.Rat).Abs(new(big.Rat).Sub(exactValue, out.Points[99].Value))
	tolerance, _ := new(big.Rat).SetString("0.000000000000000001")
	if errorValue.Cmp(tolerance) > 0 {
		t.Fatal("virtual precision exceeded tolerance", errorValue)
	}
}

func TestVirtualStrategyResumesAfterSuspensionWithCashFlows(t *testing.T) {
	days := []BenchmarkDay{
		{Date: "2022-02-25", Price: big.NewRat(100, 1)},
		{Date: "2022-02-26", ExternalFlow: big.NewRat(50, 1)},
		{Date: "2022-02-27", ExternalFlow: big.NewRat(-10, 1)},
		{Date: "2022-02-28", Price: big.NewRat(80, 1), ExternalFlow: big.NewRat(5, 1)},
		{Date: "2022-03-01", Price: big.NewRat(88, 1), ExternalFlow: new(big.Rat)},
	}
	out, e := VirtualStrategyValues(big.NewRat(100, 1), days)
	if e != nil || out.Reason != "" || out.Points[1].Value != nil || out.Points[2].Value != nil || out.Points[3].Value.Cmp(big.NewRat(125, 1)) != 0 || out.Points[4].Value.Cmp(big.NewRat(275, 2)) != 0 {
		t.Fatal("pending cash was backdated, lost or earned suspended returns", out, e)
	}
	days[2].ExternalFlow = nil
	out, e = VirtualStrategyValues(big.NewRat(100, 1), days)
	if e != nil || out.Points[4].Value != nil || out.Reason != "strategy_flow_missing" {
		t.Fatal("unknown flow invented", out, e)
	}
}

func TestStrategyAnalyticsBoundsInverseFXRunningSums(t *testing.T) {
	first := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	days := []PerformanceDay{}
	exact := new(big.Rat)
	for i := 0; i < 2573; i++ {
		flow := new(big.Rat)
		if i > 0 {
			flow.SetFrac64(1, int64(100003+i*2))
			exact.Add(exact, flow)
		}
		days = append(days, PerformanceDay{Date: first.AddDate(0, 0, i).Format(time.DateOnly), NAV: big.NewRat(1000, 1), ExternalFlow: flow, Complete: true})
	}
	out, e := CalculateStrategyAnalytics(days, nil)
	if e != nil {
		t.Fatal(e)
	}
	tolerance, _ := new(big.Rat).SetString("0.000000000000000000002")
	last := out.Points[len(out.Points)-1]
	diff := new(big.Rat).Abs(new(big.Rat).Sub(last.Contributions, exact))
	if diff.Cmp(tolerance) > 0 {
		t.Fatal("reporting precision lost", diff)
	}
	for _, p := range out.Points {
		if p.Contributions.Denom().BitLen() > 128 {
			t.Fatal("unbounded inverse FX denominator", p.Date)
		}
	}
	if last.Profit.Cmp(new(big.Rat).Neg(last.Contributions)) != 0 {
		t.Fatal("profit and contributions disagree")
	}
}
