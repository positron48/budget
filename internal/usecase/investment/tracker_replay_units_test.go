package investment

import (
	"reflect"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

func TestTrackerReplayUnitsMatchConnectedDailyReplay(t *testing.T) {
	ledgers, ops := portfolioFixture()
	for i := range ledgers {
		ledgers[i].PortfolioTracking = true
	}
	// Keep the source archived: the chart still needs its transferred units.
	ledgers[0].Archived = true
	ops[1].OccurredAt = day.Add(24 * time.Hour)
	ops[2].OccurredAt = day.Add(48 * time.Hour)
	ops[3].OccurredAt = ops[2].OccurredAt
	action := op("distribution", "distribution", "0", 2)
	action.LedgerID = "b"
	action.InstrumentKey = "S"
	action.OccurredAt = day.Add(72 * time.Hour)
	action.Note = "confirmed"
	action.Distribution = &d.InvestmentDistribution{EventKey: "event", TargetInstrumentKey: "NEW", NewUnits: "1", OldUnits: "2", CostNumerator: "1", CostDenominator: "2"}
	// Distribution is an equity action.
	ops[1].Accrued = "0"
	split := op("split", "split", "0", 3)
	split.LedgerID = "b"
	split.Currency = ""
	split.InstrumentKey = "NEW"
	split.SplitNew = "3"
	split.SplitOld = "1"
	split.OccurredAt = day.Add(96 * time.Hour)
	ops = append(ops, action, split)
	cuts := []time.Time{day.Add(-time.Hour), day.Add(12 * time.Hour), day.Add(36 * time.Hour), day.Add(60 * time.Hour), day.Add(84 * time.Hour), day.Add(108 * time.Hour)}
	full, err := ReplayPortfolio(ledgers, ops, cuts[len(cuts)-1])
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range ledgers {
		local := []d.InvestmentOperation{}
		for _, o := range ops {
			if o.LedgerID == l.ID {
				local = append(local, o)
			}
		}
		history, supported, err := TrackerSecurityLegHistory(l, local, full.Ledgers[l.ID].SecurityLegs, cuts)
		if err != nil || !supported {
			t.Fatal(supported, err)
		}
		for i, at := range cuts {
			want, err := ReplayPortfolio(ledgers, ops, at)
			if err != nil {
				t.Fatal(err)
			}
			units := func(s d.InvestmentSnapshot) map[string]string {
				out := map[string]string{}
				for _, p := range s.Positions {
					if p.Quantity != "0" {
						out[p.InstrumentKey] = p.Quantity
					}
				}
				return out
			}
			if !reflect.DeepEqual(units(history[i]), units(want.Ledgers[l.ID].Snapshot)) {
				t.Fatalf("%s %s: got %v want %v", l.ID, at, history[i], want.Ledgers[l.ID].Snapshot)
			}
		}
	}
}

func TestTrackerSecurityLegHistoryPreservesQualityBoundaries(t *testing.T) {
	l := testLedger()
	l.PortfolioTracking = true
	o := op("buy", "buy", "10", 1)
	o.InstrumentKey = "S"
	o.Quantity = "2"
	o.Timing = &d.InvestmentOperationTiming{OccurredDate: "2024-01-01"}
	legs := []d.InvestmentSecurityLeg{{OperationID: o.ID, InstrumentKey: "S", Quantity: "2", EffectiveAt: o.OccurredAt}}
	got, ok, err := TrackerSecurityLegHistory(l, []d.InvestmentOperation{o}, legs, []time.Time{day, day.Add(2 * time.Hour)})
	if err != nil || !ok || len(got[0].Issues) != 0 || !reflect.DeepEqual(got[1].Issues, []string{"date_only_timing"}) {
		t.Fatal(got, ok, err)
	}
	o.Kind = "cash_reconciliation"
	if _, ok, err = TrackerSecurityLegHistory(l, []d.InvestmentOperation{o}, legs, []time.Time{day}); ok || err != nil {
		t.Fatal("legacy quality contract must retain replay", ok, err)
	}
}
