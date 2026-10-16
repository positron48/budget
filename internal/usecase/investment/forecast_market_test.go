package investment

import (
	d "github.com/positron48/budget/internal/domain"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestMarketForecastSchedulesAndHistoricalProjectionsStayDistinctAndReadOnly(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	old := time.Date(2025, 11, 20, 0, 0, 0, 0, time.UTC)
	future := old.AddDate(1, 0, 0)
	schedule := PayoutSchedule{InstrumentKey: "S", Currency: "USD", Reference: "Synthetic schedule", Dividends: []md.Dividend{{PaymentDate: old, DividendNet: &md.BondAmount{Currency: "USD", Amount: "2"}, Regularity: "quarterly"}}, Coupons: []md.BondCoupon{{Date: future, Pay: nil}}}
	split := d.InvestmentOperation{Status: "posted", Kind: "split", InstrumentKey: "S", OccurredAt: at.AddDate(0, -1, 0), SplitNew: "2", SplitOld: "1"}
	result := MarketForecastEvents("tenant", nil, []PayoutSchedule{schedule}, []d.InvestmentOperation{split, split}, at, "2026-10")
	if len(result) != 2 {
		t.Fatal(result)
	}
	var dividend ForecastEvent
	for _, e := range result {
		if _, err := NormalizeForecastEvent(e); err != nil {
			t.Fatal(err)
		}
		if e.Kind == "dividend" {
			dividend = e
			if e.Origin != "historical_projection" || e.Nature != "forecast" || e.PerShare != "1.000000000000" {
				t.Fatal(e)
			}
		} else if e.Currency != "USD" || e.PerShare != "" || e.Origin != "provider" {
			t.Fatal(e)
		}
	}
	repeated := MarketForecastEvents("tenant", nil, []PayoutSchedule{schedule}, []d.InvestmentOperation{split}, at, "2026-10")
	if repeated[0].ID != result[0].ID || repeated[1].ID != result[1].ID {
		t.Fatal("unstable event identity")
	}
	dividend.Cancelled = true
	dividend.Origin = ""
	result = MarketForecastEvents("tenant", []ForecastEvent{dividend}, []PayoutSchedule{schedule}, nil, at, "2026-10")
	if len(result) != 2 {
		t.Fatal("cancelled user event did not suppress generated duplicate", result)
	}
	schedule.Dividends[0].DividendType = "special"
	if got := MarketForecastEvents("tenant", nil, []PayoutSchedule{schedule}, nil, at, "2026-10"); len(got) != 1 {
		t.Fatal("special dividend repeated", got)
	}
	schedule.Dividends = []md.Dividend{{PaymentDate: old, DividendNet: &md.BondAmount{Currency: "USD", Amount: "2"}}, {PaymentDate: future, DividendNet: &md.BondAmount{Currency: "USD", Amount: "3"}}}
	result = MarketForecastEvents("tenant", nil, []PayoutSchedule{schedule}, nil, at, "2026-10")
	for _, e := range result {
		if e.Kind == "dividend" && (e.Nature != "announced" || e.PerShare != "3") {
			t.Fatal("projection replaced announcement", e)
		}
	}
	schedule.Dividends = nil
	schedule.Floating = true
	schedule.Coupons[0].Pay = &md.BondAmount{Currency: "USD", Amount: "5"}
	schedule.Coupons[0].FixDate = at.AddDate(0, 0, 10)
	result = MarketForecastEvents("tenant", nil, []PayoutSchedule{schedule}, nil, at, "2026-10")
	if len(result) != 1 || result[0].Nature != "forecast" {
		t.Fatal("unfixed variable amount presented as announced", result)
	}
}
