package investmentvaluation

import (
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"math/big"
	"testing"
	"time"
)

func TestPositionRowsSeparateBookCostNativePriceAndReportingValue(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	id := componentID("ledger", "position", "BOND")
	snapshot := d.InvestmentSnapshot{Ledger: d.InvestmentLedger{ID: "ledger", TenantID: "tenant", Name: "Broker", Version: 9}, AsOf: at, Positions: []d.InvestmentPosition{{InstrumentKey: "BOND", Currency: "RUB", Quantity: "2", CleanCost: "1905", AcquiredAccrued: "40", RealizedClean: "7", RealizedAccrued: "0"}}}
	value := Result{TenantID: "tenant", At: at, LedgerVersions: map[string]int64{"ledger": 2}, Components: map[string]ComponentIdentity{id: {LedgerID: "ledger", Kind: "position", Key: "BOND", Quantity: "2"}}, InstrumentFacts: map[string]InstrumentFact{"BOND": {Terms: calc.PriceTerms{Kind: "bond", Nominal: "1000", Currency: "RUB", LotSize: "10"}, Price: &calc.PriceFact{Price: "95", Unit: "percent_of_nominal"}}}, PortfolioValue: calc.PortfolioValue{Positions: map[string]calc.NormalizedPrice{id: {Currency: "RUB", Clean: big.NewRat(1900, 1), Accrued: big.NewRat(60, 1), Dirty: big.NewRat(1960, 1)}}, Valuation: calc.Valuation{KnownValue: big.NewRat(1960, 1), Complete: true, ComponentValues: map[string]*big.Rat{id: big.NewRat(1960, 1)}}}}
	payment := PositionPayment{LedgerID: "ledger", InstrumentKey: "BOND", Currency: "EUR", Gross: big.NewRat(50, 1), Tax: big.NewRat(6, 1), Net: big.NewRat(44, 1)}
	rows, e := PositionRows([]d.InvestmentSnapshot{snapshot}, &value, InstrumentGroups{}, []PositionPayment{payment}, nil)
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	r := rows[0]
	if r.UnrealizedClean.RatString() != "-5" || r.UnrealizedDirty.RatString() != "15" || r.NativePrice.Clean.RatString() != "950" || r.NativePrice.Accrued.RatString() != "30" || r.Value.RatString() != "1960" || r.Weight.RatString() != "1" || r.Payments[0].Currency != "EUR" || r.Facts.Price.Price != "95" {
		t.Fatal(r)
	}
	if value.Positions[id].Clean.RatString() != "1900" {
		t.Fatal("mutated normalized source")
	}
	// Later journal revisions need not invalidate an earlier unchanged projection.
	if rows[0].Position.CleanCost != "1905" {
		t.Fatal("cost changed")
	}
	snapshot.Positions[0].CleanCost = ""
	rows, e = PositionRows([]d.InvestmentSnapshot{snapshot}, &value, InstrumentGroups{}, nil, nil)
	if e != nil || rows[0].UnrealizedClean != nil || rows[0].UnrealizedDirty != nil {
		t.Fatal(rows, e)
	}
	snapshot.Positions[0].CleanCost = "1905"
	snapshot.Positions[0].Currency = "USD"
	rows, e = PositionRows([]d.InvestmentSnapshot{snapshot}, &value, InstrumentGroups{}, nil, nil)
	if e != nil || rows[0].UnrealizedClean != nil || rows[0].Issues[0].Code != "book_price_currency_mismatch" {
		t.Fatal(rows, e)
	}
	snapshot.Positions[0].Quantity = "3"
	if _, e = PositionRows([]d.InvestmentSnapshot{snapshot}, &value, InstrumentGroups{}, nil, nil); e == nil {
		t.Fatal("mixed journal and valuation quantity")
	}
	snapshot.Positions[0].Quantity = "2"
	value.KnownValue = big.NewRat(1, 1)
	if _, e = PositionRows([]d.InvestmentSnapshot{snapshot}, &value, InstrumentGroups{}, nil, nil); e == nil {
		t.Fatal("corrupt projection sum accepted")
	}
	value.KnownValue = big.NewRat(1960, 1)
	value.ComponentValues = nil
	rows, e = PositionRows([]d.InvestmentSnapshot{snapshot}, &value, InstrumentGroups{}, nil, nil)
	if e != nil || rows[0].Value != nil || rows[0].NativePrice.Clean != nil || rows[0].Position.CleanCost != "1905" || rows[0].Issues[0].Code != "valuation_unavailable" {
		t.Fatal(rows, e)
	}
}
func TestPositionRowsKeepBooksWithoutStaleMarketData(t *testing.T) {
	s := d.InvestmentSnapshot{Ledger: d.InvestmentLedger{ID: "one"}, Positions: []d.InvestmentPosition{{InstrumentKey: "ABC", Currency: "RUB", Quantity: "0", CleanCost: "0", AcquiredAccrued: "0", RealizedClean: "8"}, {InstrumentKey: "UNKNOWN", Currency: "USD", Quantity: "2", CleanCost: "", AcquiredAccrued: ""}}}
	rows, e := PositionRows([]d.InvestmentSnapshot{s}, nil, InstrumentGroups{}, nil, nil)
	if e != nil || len(rows) != 2 || rows[0].Value != nil || rows[0].Position.RealizedClean != "8" || rows[0].UnrealizedClean.RatString() != "0" || rows[1].UnrealizedClean != nil {
		t.Fatal(rows, e)
	}
}
