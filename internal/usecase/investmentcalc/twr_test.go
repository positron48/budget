package investmentcalc

import (
	"math/big"
	"testing"
)

func TestTWRIndexStartsEachSegmentAt100AndPreservesExactFractions(t *testing.T) {
	for _, test := range []struct {
		point TWRPoint
		want  string
	}{
		{TWRPoint{State: "baseline", Segment: 1}, "100"},
		{TWRPoint{State: "baseline", Segment: 2}, "100"},
		{TWRPoint{State: "calculated", Segment: 1, SegmentReturn: big.NewRat(1, 3)}, "400/3"},
		{TWRPoint{State: "calculated", Segment: 1, SegmentReturn: big.NewRat(-1, 1)}, "0"},
	} {
		v := TWRIndex(test.point)
		if v == nil || v.RatString() != test.want {
			t.Fatal(v, test)
		}
	}
	for _, point := range []TWRPoint{{State: "gap", Segment: 1, SegmentReturn: big.NewRat(99, 1)}, {State: "baseline"}, {State: "calculated", Segment: 1}, {State: "calculated", Segment: 1, SegmentReturn: big.NewRat(-2, 1)}} {
		if TWRIndex(point) != nil {
			t.Fatal(point)
		}
	}
}
