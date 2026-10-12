package investmentvaluation

import (
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestJournalReplayToNAVSettlementAndHistoricalFX(t *testing.T) {
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger := d.InvestmentLedger{ID: "ledger", TenantID: "tenant", Version: 3, StartedAt: day}
	deposit := d.InvestmentOperation{ID: "deposit", LedgerID: ledger.ID, TenantID: ledger.TenantID, Kind: "deposit", Currency: "USD", Amount: "1000", OccurredAt: day.Add(time.Hour), Sequence: 1, Status: "posted"}
	buy := deposit
	buy.ID = "buy"
	buy.Kind = "buy"
	buy.Amount = "200"
	buy.Quantity = "2"
	buy.Fee = "2"
	buy.InstrumentKey = "SHARE"
	buy.Sequence = 2
	buy.OccurredAt = day.Add(2 * time.Hour)
	buy.SettlementAt = day.AddDate(0, 0, 2)
	for i, want := range []string{"79840", "89820"} {
		at := day.AddDate(0, 0, i+1)
		replay, e := inv.Replay(ledger, []d.InvestmentOperation{deposit, buy}, at)
		if e != nil {
			t.Fatal(e)
		}
		evidence := map[string]InstrumentEvidence{"SHARE": {TenantID: "tenant", Mapping: md.Mapping{InstrumentKey: "SHARE", Version: 1, Periods: []md.MappingPeriod{{Provider: "tinvest", AccessScope: "personal", InstrumentID: "share", Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: day}}}, Price: md.SelectedPrice{State: "fresh", ObservationID: "quote", ObservationRevision: 2, Fact: &calc.PriceFact{InstrumentID: "share", SourceKey: "tinvest/personal", Price: "100", Currency: "USD", Unit: "currency_per_unit", Adjustment: "unadjusted", EffectiveAt: at, AvailableAt: at, Complete: true}}}}
		rate := "80"
		if i == 1 {
			rate = "90"
		}
		fx := []calc.FXFixing{{From: "USD", To: "RUB", Rate: rate, Policy: "market@1", ID: "fx", Revision: 1, At: at, AvailableAt: at, ValidUntil: at.Add(time.Hour)}}
		got, e := Value("tenant", []d.InvestmentSnapshot{replay.Snapshot}, evidence, fx, "RUB", "market@1")
		// 798 economic cash + 200 positions = 998 USD on either side of settlement.
		if e != nil || !got.Complete || got.KnownValue.RatString() != want || got.LedgerVersions["ledger"] != 3 || len(got.FXPaths["USD"]) != 1 {
			t.Fatal(got, e)
		}
		composition, e := ComposeGroups(got, InstrumentGroups{}, nil)
		if e != nil || !composition.Complete || composition.SecuritiesKnown.RatString() != []string{"16000", "18000"}[i] || composition.CashKnown.RatString() != []string{"63840", "71820"}[i] {
			t.Fatal("historical component FX or cash counted incorrectly", composition, e)
		}
		identity := got.Components[componentID(ledger.ID, "position", "SHARE")]
		if identity.Currency != "USD" || identity.InstrumentType != "share" || identity.Quantity != "2" {
			t.Fatal(identity)
		}

	}
}
func TestSnapshotIsolationGapsAndUnknownCost(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	s := d.InvestmentSnapshot{Ledger: d.InvestmentLedger{ID: "one", TenantID: "tenant", Version: 1, StartedAt: at}, AsOf: at, Cash: []d.InvestmentCash{{Currency: "RUB", Settled: "10", Receivable: "0", Payable: "0", Economic: "999999"}}, Positions: []d.InvestmentPosition{{InstrumentKey: "S", Currency: "RUB", Quantity: "2"}}, Issues: []string{"unknown_acquisition_cost"}}
	got, e := Value("tenant", []d.InvestmentSnapshot{s}, nil, nil, "RUB", "market")
	if e != nil || got.Complete || got.KnownValue.RatString() != "10" || len(got.Warnings) != 1 || got.Issues[0].Code != "mapping_missing" {
		t.Fatal(got, e)
	}
	for _, inputs := range [][]d.InvestmentSnapshot{{s, s}, func() []d.InvestmentSnapshot { x := s; x.Ledger.TenantID = "other"; return []d.InvestmentSnapshot{x} }(), func() []d.InvestmentSnapshot {
		x := s
		x.Ledger.ID = "two"
		x.AsOf = x.AsOf.Add(time.Second)
		return []d.InvestmentSnapshot{s, x}
	}()} {
		if _, e := Value("tenant", inputs, nil, nil, "RUB", "market"); e == nil {
			t.Fatal("incompatible snapshots accepted")
		}
	}
	s.Positions = nil
	s.Issues = append(s.Issues, "unresolved_new_issue")
	got, e = Value("tenant", []d.InvestmentSnapshot{s}, nil, nil, "RUB", "market")
	if e != nil || got.Complete {
		t.Fatal("unknown quality ignored", got, e)
	}
}
func TestStaleCandidateAndJournalNominalNeverValued(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	s := d.InvestmentSnapshot{Ledger: d.InvestmentLedger{ID: "one", TenantID: "tenant", Version: 1, StartedAt: at}, AsOf: at, Positions: []d.InvestmentPosition{{InstrumentKey: "B", Currency: "RUB", Quantity: "1", OutstandingNominal: "800"}}}
	fact := &calc.PriceFact{InstrumentID: "bond", SourceKey: "tinvest/personal", Price: "100", Currency: "RUB", Unit: "percent_of_nominal", Adjustment: "unadjusted", EffectiveAt: at, AvailableAt: at, Complete: true}
	ev := InstrumentEvidence{TenantID: "tenant", Mapping: md.Mapping{InstrumentKey: "B", Version: 1, Periods: []md.MappingPeriod{{Provider: "tinvest", AccessScope: "personal", InstrumentID: "bond", Kind: "bond", Currency: "RUB", Unit: "percent_of_nominal", Nominal: "1000", NominalCurrency: "RUB", From: at}}}, Price: md.SelectedPrice{State: "fresh", Fact: fact, ObservationID: "quote", ObservationRevision: 1}, Accrued: &calc.AccruedFact{InstrumentID: "bond", SourceID: "issuer", Currency: "RUB", AmountPerUnit: "0", From: at, To: at.Add(time.Hour)}}
	got, e := Value("tenant", []d.InvestmentSnapshot{s}, map[string]InstrumentEvidence{"B": ev}, nil, "RUB", "market")
	if e != nil || got.Complete || got.Issues[0].Code != "journal_nominal_conflict" {
		t.Fatal(got, e)
	}
	ev.Mapping.Periods[0].Nominal = "800"
	got, e = Value("tenant", []d.InvestmentSnapshot{s}, map[string]InstrumentEvidence{"B": ev}, nil, "RUB", "market")
	if e != nil || !got.Complete || got.KnownValue.RatString() != "800" {
		t.Fatal(got, e)
	}
	ev.Price.State = "stale"
	ev.Price.Candidate = fact
	ev.Price.Issues = []string{"price_stale"}
	got, e = Value("tenant", []d.InvestmentSnapshot{s}, map[string]InstrumentEvidence{"B": ev}, nil, "RUB", "market")
	if e != nil || got.Complete || got.KnownValue.Sign() != 0 || got.Issues[0].Code != "price_stale" {
		t.Fatal(got, e)
	}
}
func TestMoscowDayCutoffHistoricalOffsets(t *testing.T) {
	for date, want := range map[string]string{"2024-01-01": "2024-01-01T20:59:59.999999Z", "2013-01-01": "2013-01-01T19:59:59.999999Z"} {
		got, e := DayCutoff(date)
		if e != nil || got.Format(time.RFC3339Nano) != want {
			t.Fatal(got, e)
		}
	}
	if _, e := DayCutoff("2024-02-30"); e == nil {
		t.Fatal("invalid day")
	}
}
