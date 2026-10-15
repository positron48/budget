package investment

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

func positiveUnits(s d.InvestmentSnapshot) map[string]string {
	out := map[string]string{}
	for _, p := range s.Positions {
		if rational(p.Quantity).Sign() > 0 {
			out[p.InstrumentKey] = p.Quantity
		}
	}
	return out
}

func TestTrackerUnitHistoryMatchesFullReplay(t *testing.T) {
	l := testLedger()
	l.PortfolioTracking = true
	rng := rand.New(rand.NewSource(197))
	ops := []d.InvestmentOperation{}
	held := map[string]int{}
	for i := 1; i <= 400; i++ {
		key := fmt.Sprintf("S%d", rng.Intn(10))
		kind := "buy"
		if held[key] > 0 && rng.Intn(3) == 0 {
			kind = "sell"
			held[key]--
		} else {
			held[key]++
		}
		o := op(fmt.Sprint(i), kind, "10", int64(i))
		o.InstrumentKey = key
		o.Quantity = "1"
		o.OccurredAt = day.Add(time.Duration(i) * time.Hour)
		ops = append(ops, o)
	}
	// Fractional and reverse ratios must not be accumulated from rounded DB legs.
	for i, ratio := range []string{"0.3333333333", "7", "0.125"} {
		o := op(fmt.Sprintf("split%d", i), "split", "0", int64(401+i))
		o.Currency = ""
		o.InstrumentKey = "S1"
		o.SplitOld = "1"
		o.SplitNew = ratio
		o.OccurredAt = day.Add(time.Duration(401+i) * time.Hour)
		ops = append(ops, o)
	}
	void := op("void", "sell", "10", 405)
	void.InstrumentKey = "S1"
	void.Quantity = "99999"
	void.Status = "voided"
	ops = append(ops, void)
	// Out-of-order storage is normal after backdated corrections.
	rng.Shuffle(len(ops), func(i, j int) { ops[i], ops[j] = ops[j], ops[i] })
	cuts := []time.Time{day.Add(-time.Hour)}
	for i := 0; i < 21; i++ {
		cuts = append(cuts, day.Add(time.Duration(i)*24*time.Hour))
	}
	got, supported, e := TrackerUnitHistory(l, ops, cuts)
	if e != nil || !supported {
		t.Fatal(supported, e)
	}
	for i, at := range cuts {
		full, e := ReplayPortfolio([]d.InvestmentLedger{l}, ops, at)
		if e != nil {
			t.Fatal(e)
		}
		want := positiveUnits(full.Ledgers[l.ID].Snapshot)
		if !reflect.DeepEqual(positiveUnits(got[i]), want) {
			t.Fatalf("%s: got %v want %v", at, positiveUnits(got[i]), want)
		}
	}
}

func TestTrackerUnitHistoryRejectsFutureShortAndFallsBackForCorporateBooks(t *testing.T) {
	l := testLedger()
	l.PortfolioTracking = true
	buy := op("buy", "buy", "10", 1)
	buy.InstrumentKey = "S"
	buy.Quantity = "1"
	sell := op("sell", "sell", "10", 2)
	sell.InstrumentKey = "S"
	sell.Quantity = "2"
	sell.OccurredAt = day.AddDate(0, 0, 40)
	if _, supported, e := TrackerUnitHistory(l, []d.InvestmentOperation{buy, sell}, []time.Time{day}); !supported || !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(supported, e)
	}
	buy.Kind = "security_transfer_in"
	if _, supported, e := TrackerUnitHistory(l, []d.InvestmentOperation{buy}, []time.Time{day}); supported || e != nil {
		t.Fatal(supported, e)
	}
	l.PortfolioTracking = false
	if _, supported, e := TrackerUnitHistory(l, nil, []time.Time{day}); supported || e != nil {
		t.Fatal(supported, e)
	}
}

func BenchmarkTrackerUnitHistory10000Operations500Instruments(b *testing.B) {
	l := testLedger()
	l.PortfolioTracking = true
	ops := make([]d.InvestmentOperation, 10000)
	for i := range ops {
		o := op(fmt.Sprint(i), "buy", "10", int64(i+1))
		o.InstrumentKey = fmt.Sprintf("S%d", i%500)
		o.Quantity = "1"
		o.OccurredAt = day.Add(time.Duration(i) * time.Hour)
		ops[i] = o
	}
	cuts := make([]time.Time, 31)
	for i := range cuts {
		cuts[i] = day.Add(time.Duration(10000+i*24) * time.Hour)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, e := TrackerUnitHistory(l, ops, cuts); e != nil {
			b.Fatal(e)
		}
	}
}

func TestRecordedTrackerHistoryPredatesMetadataCreation(t *testing.T) {
	l := testLedger()
	l.PortfolioTracking = true
	l.StartedAt = day.AddDate(2, 0, 0)
	buy := op("earlier-buy", "buy", "10", 1)
	buy.Quantity, buy.InstrumentKey = "2", "HISTORY"
	buy.OccurredAt = day.Add(time.Hour)
	cuts := []time.Time{day.Add(-time.Hour), day.Add(2 * time.Hour)}
	got, supported, err := TrackerUnitHistory(l, []d.InvestmentOperation{buy}, cuts)
	if err != nil || !supported {
		t.Fatal(supported, err)
	}
	if len(got[0].Positions) != 0 || positiveUnits(got[1])["HISTORY"] != "2" {
		t.Fatal(got)
	}
	for i, at := range cuts {
		replay, err := ReplayPortfolio([]d.InvestmentLedger{l}, []d.InvestmentOperation{buy}, at)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(positiveUnits(got[i]), positiveUnits(replay.Ledgers[l.ID].Snapshot)) {
			t.Fatal("fast and full history disagree", at)
		}
	}
	opening := buy
	opening.Kind = "opening_position"
	if RecordedTrackerStartsFromZero(l, []d.InvestmentOperation{opening}) {
		t.Fatal("opening balance cannot prove earlier history")
	}
	if !RecordedTrackerStartsFromZero(l, nil) {
		t.Fatal("empty recorded strategy starts at zero")
	}
	l.PortfolioTracking = false
	if RecordedTrackerStartsFromZero(l, nil) {
		t.Fatal("legacy cash book must retain start boundary")
	}
}
