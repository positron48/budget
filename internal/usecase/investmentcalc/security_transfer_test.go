package investmentcalc

import (
	"math/big"
	"reflect"
	"testing"
)

func TestExtractFIFOAndReceiveSlicesPreserveCostsAndAtomicity(t *testing.T) {
	var source, destination Book
	if e := source.BuyCost("origin", "3", "1"); e != nil {
		t.Fatal(e)
	}
	incoming, e := source.ExtractFIFO("2")
	if e != nil || incoming[0].Cost.RatString() != "2/3" {
		t.Fatal(incoming, e)
	}
	incoming[0].OriginID = "slice"
	if e = destination.ReceiveSlices(incoming, []string{"slice"}); e != nil {
		t.Fatal(e)
	}
	before := destination.Lots()
	incoming[0].Cost.SetInt64(99)
	if destination.Lots()[0].Cost.RatString() != "2/3" {
		t.Fatal("incoming alias")
	}
	for _, order := range [][]string{{}, {"slice", "duplicate"}, {"wrong"}} {
		if e = destination.ReceiveSlices([]Lot{{OriginID: "another", Quantity: big.NewRat(1, 1), Cost: big.NewRat(1, 7)}}, order); e == nil || !reflect.DeepEqual(before, destination.Lots()) {
			t.Fatal(e, destination.Lots())
		}
	}
	sourceBefore := source.Lots()
	if _, e = source.ExtractFIFO("2"); e == nil || !reflect.DeepEqual(sourceBefore, source.Lots()) {
		t.Fatal("overdraw changed source", e)
	}
	// Original and returned parts remain two cost layers, even if both refer to
	// one acquisition in the journal's provenance map.
	if e = destination.ReceiveSlices([]Lot{{OriginID: "returned", Quantity: big.NewRat(1, 1), Cost: big.NewRat(0, 1)}}, []string{"slice", "returned"}); e != nil {
		t.Fatal(e)
	}
	matches, _, e := destination.SellProceeds("2", "0")
	if e != nil || len(matches) != 1 || matches[0].Cost.RatString() != "2/3" || destination.Lots()[0].OriginID != "returned" {
		t.Fatal(matches, e)
	}
}
