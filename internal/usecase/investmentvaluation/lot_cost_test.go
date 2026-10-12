package investmentvaluation

import (
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"math/big"
	"testing"
	"time"
)

func TestHistoricalLotCostUsesEachAcquisitionDayExactly(t *testing.T) {
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	converter := LotCostConverter{Policy: "market"}
	for i, rate := range []string{"3", "10", "999"} {
		at := day.AddDate(0, 0, i)
		converter.Fixings = append(converter.Fixings, calc.FXFixing{ID: at.String(), Revision: 1, From: "USD", To: "RUB", Rate: rate, Policy: "market", At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour)})
	}
	lots := []inv.RemainingLot{{AcquiredAt: day, CleanCost: big.NewRat(2, 3), AcquiredAccrued: big.NewRat(0, 1)}, {AcquiredAt: day.AddDate(0, 0, 1), CleanCost: big.NewRat(1, 3), AcquiredAccrued: big.NewRat(1, 5)}}
	got, e := converter.Convert(lots, "USD", "RUB")
	if e != nil || !got.CleanComplete || !got.AccruedComplete || got.Clean.RatString() != "16/3" || got.Accrued.RatString() != "2" {
		t.Fatal(got, e)
	}
	// Missing date and missing FX are distinct from unknown cost and actual zero.
	lots = append(lots, inv.RemainingLot{CleanCost: big.NewRat(1, 1), AcquiredAccrued: big.NewRat(0, 1)}, inv.RemainingLot{AcquiredAt: day.AddDate(0, 0, 4), CleanCost: big.NewRat(1, 1), AcquiredAccrued: big.NewRat(0, 1)}, inv.RemainingLot{AcquiredAt: day, AcquiredAccrued: big.NewRat(0, 1)})
	got, e = converter.Convert(lots, "USD", "RUB")
	if e != nil || got.CleanComplete || !got.AccruedComplete || got.Clean.RatString() != "16/3" || len(got.Reasons) != 3 {
		t.Fatal(got, e)
	}
	got, e = converter.Convert([]inv.RemainingLot{{CleanCost: big.NewRat(4, 1), AcquiredAccrued: big.NewRat(0, 1)}}, "USD", "USD")
	if e != nil || !got.CleanComplete || got.Clean.RatString() != "4" {
		t.Fatal(got, e)
	}
	got, e = converter.Convert([]inv.RemainingLot{{}}, "USD", "RUB")
	if e != nil || got.Clean != nil || got.Accrued != nil || got.CleanComplete {
		t.Fatal(got, e)
	}
	got, e = converter.Convert(nil, "USD", "RUB")
	if e != nil || got.Clean.Sign() != 0 || !got.CleanComplete {
		t.Fatal(got, e)
	}
}

func TestAcquisitionFXEvidenceMatchesExactCostAndUnknowns(t *testing.T) {
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := LotCostConverter{Policy: "market", Fixings: []calc.FXFixing{{ID: "old", Revision: 2, From: "USD", To: "RUB", Rate: "3", Policy: "market", At: day, AvailableAt: day, ValidUntil: day.Add(24 * time.Hour)}, {ID: "new", Revision: 1, From: "USD", To: "RUB", Rate: "999", Policy: "market", At: day.Add(24 * time.Hour), AvailableAt: day.Add(24 * time.Hour), ValidUntil: day.Add(48 * time.Hour)}}}
	lot := inv.RemainingLot{AcquiredAt: day, CleanCost: big.NewRat(2, 3), AcquiredAccrued: big.NewRat(0, 1)}
	fx, at, e := c.AcquisitionFX(lot, "RUB", "USD")
	if e != nil || fx == nil || fx.Rate.RatString() != "1/3" || len(fx.Legs) != 1 || fx.Legs[0].ID != "old" || fx.Legs[0].Revision != 2 || !fx.Legs[0].Inverse || at.IsZero() {
		t.Fatal(fx, at, e)
	}
	converted, e := c.Convert([]inv.RemainingLot{lot}, "RUB", "USD")
	if e != nil || converted.Clean.Cmp(new(big.Rat).Mul(lot.CleanCost, fx.Rate)) != 0 {
		t.Fatal(converted, e)
	}
	for _, row := range []inv.RemainingLot{{AcquiredAt: day}, {AcquiredAt: day, CleanCost: big.NewRat(0, 1)}, {CleanCost: big.NewRat(1, 1)}, {AcquiredAt: day.Add(72 * time.Hour), CleanCost: big.NewRat(1, 1)}} {
		v, _, e := c.AcquisitionFX(row, "RUB", "USD")
		if e != nil || v != nil {
			t.Fatal(v, e)
		}
	}
}
