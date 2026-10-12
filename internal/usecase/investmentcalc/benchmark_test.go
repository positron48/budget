package investmentcalc

import (
	"math/big"
	"testing"
)

func benchmarkDay(date, price, flow string, trade bool) BenchmarkDay {
	point := BenchmarkDay{Date: date, CanTrade: trade}
	if price != "" {
		point.Price = performanceNumber(price)
	}
	if flow != "" {
		point.ExternalFlow = performanceNumber(flow)
	}
	return point
}
func benchmarkEqual(t *testing.T, value *big.Rat, want string) {
	t.Helper()
	expected := performanceNumber(want)
	if value == nil || value.Cmp(expected) != 0 {
		t.Fatalf("got %v, want %v", value, expected)
	}
}
func TestBenchmarkSameExternalFlows(t *testing.T) {
	days := []BenchmarkDay{benchmarkDay("2026-01-01", "100", "999", true), benchmarkDay("2026-01-02", "110", "500", true), benchmarkDay("2026-01-03", "121", "-600", true)}
	out, err := BenchmarkPortfolio("RUB", big.NewRat(1000, 1), days)
	if err != nil || out.Reason != "" {
		t.Fatal(out, err)
	}
	benchmarkEqual(t, out.Points[0].Value, "1000")
	benchmarkEqual(t, out.Points[1].Value, "1600")
	benchmarkEqual(t, out.Points[2].Value, "1160")
	benchmarkEqual(t, out.Points[2].Index, "121")
	benchmarkEqual(t, out.Points[2].Cash, "0")
	// Neither caller inputs nor previously emitted units may be mutated.
	benchmarkEqual(t, days[1].ExternalFlow, "500")
	benchmarkEqual(t, out.Points[0].Units, "10")
}
func TestBenchmarkWaitsForExecutableSession(t *testing.T) {
	out, err := BenchmarkPortfolio("USD", big.NewRat(1000, 1), []BenchmarkDay{
		benchmarkDay("2026-01-01", "100", "0", true),
		benchmarkDay("2026-01-02", "100", "500", false),
		benchmarkDay("2026-01-03", "", "0", false),
		benchmarkDay("2026-01-04", "125", "0", true),
	})
	if err != nil {
		t.Fatal(err)
	}
	benchmarkEqual(t, out.Points[1].Units, "10")
	benchmarkEqual(t, out.Points[1].Cash, "500")
	benchmarkEqual(t, out.Points[1].Value, "1500")
	if out.Points[2].Value != nil || out.Points[2].State != "gap" {
		t.Fatal("future price leaked backward", out.Points[2])
	}
	benchmarkEqual(t, out.Points[3].Units, "14")
	benchmarkEqual(t, out.Points[3].Value, "1750")
}
func TestBenchmarkPendingWithdrawalAndInsufficientBalance(t *testing.T) {
	for _, trade := range []bool{false, true} {
		out, err := BenchmarkPortfolio("RUB", big.NewRat(1000, 1), []BenchmarkDay{benchmarkDay("2026-01-01", "100", "0", true), benchmarkDay("2026-01-02", "100", "-1100", trade), benchmarkDay("2026-01-03", "150", "0", true)})
		if err != nil || out.Reason != "insufficient_benchmark_balance" || out.Points[1].Value != nil || out.Points[2].State != "stopped" {
			t.Fatal(out, err)
		}
		benchmarkEqual(t, out.Points[2].Index, "150")
	}
	out, err := BenchmarkPortfolio("EUR", big.NewRat(1000, 1), []BenchmarkDay{benchmarkDay("2026-01-01", "100", "0", true), benchmarkDay("2026-01-02", "", "-500", false), benchmarkDay("2026-01-03", "125", "0", true)})
	if err != nil || out.Points[1].State != "pending" || out.Points[1].Value != nil {
		t.Fatal(out, err)
	}
	benchmarkEqual(t, out.Points[1].PendingWithdrawal, "500")
	benchmarkEqual(t, out.Points[1].Units, "10")
	benchmarkEqual(t, out.Points[2].Units, "6")
	benchmarkEqual(t, out.Points[2].Value, "750")
}
func TestBenchmarkCashWithdrawalAndUnknownFlow(t *testing.T) {
	out, err := BenchmarkPortfolio("RUB", big.NewRat(1000, 1), []BenchmarkDay{benchmarkDay("2026-01-01", "100", "0", true), benchmarkDay("2026-01-02", "100", "500", false), benchmarkDay("2026-01-03", "100", "-200", false), benchmarkDay("2026-01-04", "110", "", true), benchmarkDay("2026-01-05", "120", "0", true)})
	if err != nil || out.Reason != "external_flow_unknown" {
		t.Fatal(out, err)
	}
	benchmarkEqual(t, out.Points[2].Cash, "300")
	benchmarkEqual(t, out.Points[2].PendingWithdrawal, "0")
	benchmarkEqual(t, out.Points[2].Value, "1300")
	if out.Points[4].Value != nil {
		t.Fatal("restarted after unknown flow")
	}
	benchmarkEqual(t, out.Points[4].Index, "120")
}
func TestBenchmarkFullWithdrawalAndInputValidation(t *testing.T) {
	out, err := BenchmarkPortfolio("RUB", big.NewRat(1000, 1), []BenchmarkDay{benchmarkDay("2026-01-01", "100", "0", true), benchmarkDay("2026-01-02", "110", "-1100", true), benchmarkDay("2026-01-03", "", "500", false)})
	if err != nil {
		t.Fatal(err)
	}
	benchmarkEqual(t, out.Points[1].Units, "0")
	benchmarkEqual(t, out.Points[1].Value, "0")
	benchmarkEqual(t, out.Points[2].Value, "500")
	out, err = BenchmarkPortfolio("RUB", big.NewRat(1000, 1), []BenchmarkDay{benchmarkDay("2026-01-01", "", "0", false), benchmarkDay("2026-01-02", "100", "0", true)})
	if err != nil || out.Reason != "initial_benchmark_price_unavailable" || out.Points[1].Index != nil {
		t.Fatal("invented later baseline", out, err)
	}
	for _, days := range [][]BenchmarkDay{
		{benchmarkDay("2026-01-01", "0", "0", true)},
		{benchmarkDay("2026-01-01", "", "0", true)},
		{benchmarkDay("2026-01-01", "100", "0", true), benchmarkDay("2026-01-03", "110", "0", true)},
	} {
		if _, err = BenchmarkPortfolio("RUB", big.NewRat(1000, 1), days); err == nil {
			t.Fatal("accepted invalid evidence")
		}
	}
}

func TestBenchmarkCannotWithdrawFromEmptyPortfolioWithoutPrice(t *testing.T) {
	out, err := BenchmarkPortfolio("RUB", new(big.Rat), []BenchmarkDay{benchmarkDay("2026-01-01", "100", "0", true), benchmarkDay("2026-01-02", "", "-1", false)})
	if err != nil || out.Reason != "insufficient_benchmark_balance" {
		t.Fatal(out, err)
	}
}

func TestBenchmarkOpeningHistoryReset(t *testing.T) {
	days := []BenchmarkDay{benchmarkDay("2026-01-01", "100", "0", true), benchmarkDay("2026-01-02", "110", "0", true), benchmarkDay("2026-01-03", "120", "0", true)}
	days[1].HistoryReset = true
	out, e := BenchmarkPortfolio("RUB", big.NewRat(1000, 1), days)
	if e != nil || out.Reason != "history_reset" || out.Points[1].Value != nil || out.Points[2].Value != nil {
		t.Fatal(out, e)
	}
	benchmarkEqual(t, out.Points[2].Index, "120")
}

func TestBenchmarkCarriedInitialValuation(t *testing.T) {
	out, e := BenchmarkPortfolio("RUB", big.NewRat(1500, 1), []BenchmarkDay{benchmarkDay("2026-01-01", "100", "0", false), benchmarkDay("2026-01-02", "130", "0", true)})
	if e != nil || out.Reason != "" {
		t.Fatal(out, e)
	}
	benchmarkEqual(t, out.Points[0].Units, "15")
	benchmarkEqual(t, out.Points[1].Value, "1950")
}
