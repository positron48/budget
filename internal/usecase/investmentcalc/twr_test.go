package investmentcalc

import (
	"math/big"
	"testing"
	"time"
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

func BenchmarkDailyTWRTenYearsWithDailyFlows(b *testing.B) {
	start := time.Date(2014, 1, 1, 0, 0, 0, 0, time.UTC)
	days := make([]PerformanceDay, 3661)
	for i := range days {
		days[i] = PerformanceDay{Date: start.AddDate(0, 0, i).Format(time.DateOnly), NAV: big.NewRat(int64(10000000+i*107+i%17), 100), ExternalFlow: big.NewRat(1, 1), Complete: true}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := DailyTWR(days)
		if err != nil || result.Return == nil {
			b.Fatal(result.Reason, err)
		}
	}
}
