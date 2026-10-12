package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

func lotOperation() d.InvestmentOperation {
	o := op("buy", "buy", "100", 2)
	o.InstrumentKey = "stock"
	o.Quantity = "0"
	o.LotConversion = &d.InvestmentLotConversion{Lots: "2", LotSize: "10", TermsID: "stock:0", MappingVersion: 1, Provider: "tinvest", AccessScope: "personal", InstrumentID: "11111111-1111-4111-8111-111111111111", Evidence: "Reviewed exchange schedule", From: day, To: day.AddDate(0, 1, 0)}
	return o
}
func TestLotConversionExactUnitsAndPrecision(t *testing.T) {
	o := lotOperation()
	o.LotConversion.Lots = "9007199254740993.123456789"
	o.LotConversion.LotSize = "10"
	if e := ValidateOperation(&o); e != nil || o.Quantity != "90071992547409931.23456789" {
		t.Fatal(o, e)
	}
	o = lotOperation()
	o.LotConversion.Lots = "0.0000000001"
	o.LotConversion.LotSize = "0.0000000001"
	if ValidateOperation(&o) == nil {
		t.Fatal("lossy conversion rounded into a valid quantity")
	}
	o = lotOperation()
	o.LotConversion.Lots = "999999999999999999"
	o.LotConversion.LotSize = "10"
	if ValidateOperation(&o) == nil {
		t.Fatal("quantity overflow accepted")
	}
	o = lotOperation()
	o.Quantity = "21"
	if ValidateOperation(&o) == nil {
		t.Fatal("contradictory unit quantity accepted")
	}
}
func TestLotConversionBoundsAndReplayFreeze(t *testing.T) {
	o := lotOperation()
	o.OccurredAt = day
	if e := ValidateOperation(&o); e != nil || o.Quantity != "20" {
		t.Fatal(o, e)
	}
	end := o.LotConversion.To
	o.OccurredAt = end
	if ValidateOperation(&o) == nil {
		t.Fatal("exclusive period end accepted")
	}
	o = lotOperation()
	o.OccurredAt = day.Add(-time.Microsecond)
	if ValidateOperation(&o) == nil {
		t.Fatal("terms projected into past")
	}
	o = lotOperation()
	fund := op("fund", "deposit", "1000", 1)
	r, e := Replay(testLedger(), []d.InvestmentOperation{fund, o}, day.Add(3*time.Hour))
	if e != nil || r.Snapshot.Positions[0].Quantity != "20" {
		t.Fatal(r, e)
	}
	o.Kind = "deposit"
	o.InstrumentKey = ""
	if ValidateOperation(&o) == nil {
		t.Fatal("cash operation accepted lot terms")
	}
}
