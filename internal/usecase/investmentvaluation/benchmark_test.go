package investmentvaluation

import (
	"math/big"
	"testing"
	"time"

	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func benchmarkEvidenceFixture() (BenchmarkConvention, []BenchmarkDailyEvidence, []calc.FXFixing) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := BenchmarkConvention{Key: "proxy", Title: "Explicit ETF price proxy", Kind: "etf_price_proxy", Currency: "USD", SourceKey: "reviewed-source", InstrumentID: "fund", Unit: "currency_per_unit", Evidence: "Verified fixture convention", Revision: 1, Confirmed: true, From: start}
	points := []BenchmarkDailyEvidence{}
	fx := []calc.FXFixing{}
	for i, rate := range []string{"90", "100", "110"} {
		at := start.AddDate(0, 0, i)
		close := at.Add(18 * time.Hour)
		price := "10"
		if i == 1 {
			close = start.Add(18 * time.Hour)
		}
		if i == 2 {
			price = "12"
		}
		points = append(points, BenchmarkDailyEvidence{Date: at.Format(time.DateOnly), ExternalFlow: new(big.Rat), Price: md.SelectedPrice{State: "fresh", ObservationID: close.String(), ObservationRevision: 2, CalendarID: "calendar", CalendarRevision: 3, Fact: &calc.PriceFact{InstrumentID: c.InstrumentID, SourceKey: c.SourceKey, Price: price, Currency: "USD", Unit: c.Unit, Adjustment: "unadjusted", EffectiveAt: close, AvailableAt: close.Add(time.Hour), Complete: true}}})
		fx = append(fx, calc.FXFixing{ID: at.String(), Revision: 1, From: "USD", To: "RUB", Rate: rate, Policy: "market@1", At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour)})
	}
	points[1].ExternalFlow = big.NewRat(500, 1)
	return c, points, fx
}
func TestPrepareBenchmarkHistoricalFXAndSession(t *testing.T) {
	c, points, fx := benchmarkEvidenceFixture()
	prepared, err := PrepareBenchmark(c, "RUB", "market@1", points, fx)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int64{900, 1000, 1320} {
		if prepared.Days[i].Price.Cmp(big.NewRat(want, 1)) != 0 || prepared.Days[i].CanTrade != (i != 1) || len(prepared.Evidence[i].FX) != 1 || prepared.Evidence[i].CalendarRevision != 3 {
			t.Fatal(prepared)
		}
	}
	result, err := calc.BenchmarkPortfolio("RUB", big.NewRat(900, 1), prepared.Days)
	if err != nil || result.Points[1].Cash.Cmp(big.NewRat(500, 1)) != 0 || result.Points[2].Value.Cmp(big.NewRat(1820, 1)) != 0 {
		t.Fatal(result, err)
	}
	prepared.Days[1].ExternalFlow.SetInt64(0)
	if points[1].ExternalFlow.Sign() == 0 {
		t.Fatal("mutated caller cash flow")
	}
}
func TestPrepareBenchmarkMissingEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, issue string
		change      func(*BenchmarkConvention, []BenchmarkDailyEvidence, []calc.FXFixing)
	}{
		{"fx", "benchmark_fx_missing", func(c *BenchmarkConvention, p []BenchmarkDailyEvidence, f []calc.FXFixing) {
			f[1].ValidUntil = f[1].At.Add(time.Hour)
		}},
		{"stale", "benchmark_price_unavailable", func(c *BenchmarkConvention, p []BenchmarkDailyEvidence, f []calc.FXFixing) {
			p[1].Price.State = "stale"
			p[1].Price.Candidate = p[1].Price.Fact
		}},
		{"adjustment", "benchmark_adjustment_unconfirmed", func(c *BenchmarkConvention, p []BenchmarkDailyEvidence, f []calc.FXFixing) {
			p[1].Price.Fact.Adjustment = "provider_unspecified"
		}},
		{"currency", "benchmark_price_convention_conflict", func(c *BenchmarkConvention, p []BenchmarkDailyEvidence, f []calc.FXFixing) {
			p[1].Price.Fact.Currency = "EUR"
		}},
		{"calendar", "benchmark_calendar_evidence_missing", func(c *BenchmarkConvention, p []BenchmarkDailyEvidence, f []calc.FXFixing) {
			p[1].Price.CalendarID = ""
		}},
		{"future", "benchmark_price_unavailable", func(c *BenchmarkConvention, p []BenchmarkDailyEvidence, f []calc.FXFixing) {
			p[1].Price.Fact.AvailableAt = p[2].Price.Fact.AvailableAt
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, p, f := benchmarkEvidenceFixture()
			tc.change(&c, p, f)
			result, err := PrepareBenchmark(c, "RUB", "market@1", p, f)
			if err != nil {
				t.Fatal(err)
			}
			if result.Days[1].Price != nil || result.Days[1].CanTrade {
				t.Fatal("valued missing evidence", result)
			}
			found := false
			for _, issue := range result.Evidence[1].Issues {
				found = found || issue == tc.issue
			}
			if !found {
				t.Fatal(result.Evidence[1])
			}
		})
	}
}
func TestPrepareBenchmarkIdentityAndSeriesType(t *testing.T) {
	c, p, f := benchmarkEvidenceFixture()
	c.Kind = "total_return_index"
	if _, err := PrepareBenchmark(c, "RUB", "market@1", p, f); err == nil {
		t.Fatal("invented TR support")
	}
	c.Kind = "price_index"
	c.Unit = "index_points"
	for i := range p {
		p[i].Price.Fact.Unit = "index_points"
	}
	if _, err := PrepareBenchmark(c, "USD", "market@1", p, nil); err != nil {
		t.Fatal(err)
	}
	p[0].Price.Fact.SourceKey = "other-source"
	if _, err := PrepareBenchmark(c, "USD", "market@1", p, nil); err == nil {
		t.Fatal("mixed sources")
	}
}
