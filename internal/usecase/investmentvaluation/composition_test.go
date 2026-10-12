package investmentvaluation

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

func TestCompositionUnionCashAndExactHistoricalAmounts(t *testing.T) {
	a, b, g, h := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	groups := InstrumentGroups{Version: 1, Groups: []InstrumentGroup{{g, "Strategy"}, {h, "Other"}}, Memberships: []GroupMembership{{a, "ABC", g}, {b, "ABC", h}}}
	r := Result{PortfolioValue: calc.PortfolioValue{Valuation: calc.Valuation{KnownValue: big.NewRat(100, 1), Complete: true, ComponentValues: map[string]*big.Rat{"a": big.NewRat(30, 1), "b": big.NewRat(60, 1), "cash": big.NewRat(10, 1)}}}, Components: map[string]ComponentIdentity{"a": {LedgerID: a, Kind: "position", Key: "ABC"}, "b": {LedgerID: b, Kind: "position", Key: "ABC"}, "cash": {LedgerID: a, Kind: "cash", Key: "RUB"}}}
	got, e := ComposeGroups(r, groups, []string{g, g})
	if e != nil || got.SecuritiesKnown.RatString() != "30" || got.CashKnown.RatString() != "10" || !got.SecuritiesComplete || len(got.Segments) != 3 {
		t.Fatal(got, e)
	}
	sum := new(big.Rat)
	for _, s := range got.Segments {
		if s.Weight == nil {
			t.Fatal("missing full-portfolio weight")
		}
		sum.Add(sum, s.Weight)
	}
	if sum.Cmp(big.NewRat(1, 1)) != 0 {
		t.Fatal(sum)
	}
	got, e = ComposeGroups(r, groups, []string{g, h})
	if e != nil || got.SecuritiesKnown.RatString() != "90" {
		t.Fatal(got, e)
	}
	// Reclassification applies to the same historical component values.
	groups.Memberships = []GroupMembership{{a, "ABC", g}, {b, "ABC", g}}
	got, e = ComposeGroups(r, groups, []string{g})
	if e != nil || got.SecuritiesKnown.RatString() != "90" {
		t.Fatal(got, e)
	}
	if r.KnownValue.RatString() != "100" || r.ComponentValues["a"].RatString() != "30" {
		t.Fatal("mutated NAV")
	}
	r.ComponentValues["b"] = nil
	r.KnownValue = big.NewRat(40, 1)
	r.Complete = false
	r.Issues = []calc.QualityIssue{{ComponentID: "b", Code: "fx_missing"}}
	got, e = ComposeGroups(r, groups, []string{g})
	if e != nil || got.SecuritiesComplete || got.SecuritiesKnown.RatString() != "30" {
		t.Fatal(got, e)
	}
	for _, s := range got.Segments {
		if s.Weight != nil {
			t.Fatal("partial weight")
		}
	}
	groups.Memberships[1].GroupID = h
	got, e = ComposeGroups(r, groups, []string{g})
	if e != nil || !got.SecuritiesComplete || len(got.Issues) != 0 {
		t.Fatal("unselected issue polluted securities", got, e)
	}
	r.Issues = append(r.Issues, calc.QualityIssue{ComponentID: b, Code: "ledger_history_missing"})
	got, e = ComposeGroups(r, groups, []string{g})
	if e != nil || got.SecuritiesComplete {
		t.Fatal("coverage gap hidden", got, e)
	}
}
func TestCompositionUnknownLegacyEmptyAndNegativeCash(t *testing.T) {
	got, e := ComposeGroups(Result{}, InstrumentGroups{}, nil)
	if e != nil || got.SecuritiesKnown != nil || got.Complete || got.Issues[0].Code != "component_breakdown_missing" {
		t.Fatal(got, e)
	}
	// Empty maps must survive storage: an empty account is a known zero.
	v, e := calc.ValueExact(nil, nil, "RUB", "market", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	r := Result{PortfolioValue: calc.PortfolioValue{Valuation: v}, Components: map[string]ComponentIdentity{}}
	raw, _ := json.Marshal(r)
	r = Result{}
	if e = json.Unmarshal(raw, &r); e != nil {
		t.Fatal(e)
	}
	got, e = ComposeGroups(r, InstrumentGroups{}, nil)
	if e != nil || !got.Complete || got.SecuritiesKnown.Sign() != 0 {
		t.Fatal(got, e)
	}
	r.ComponentValues["cash"] = big.NewRat(-10, 1)
	r.KnownValue = big.NewRat(-10, 1)
	r.Components["cash"] = ComponentIdentity{Kind: "cash"}
	got, e = ComposeGroups(r, InstrumentGroups{}, nil)
	if e != nil || !got.Complete || got.Segments[0].Weight != nil {
		t.Fatal(got, e)
	}
	r.Components["positiveCash"] = ComponentIdentity{Kind: "cash", Key: "USD"}
	r.ComponentValues["positiveCash"] = big.NewRat(20, 1)
	r.KnownValue = big.NewRat(10, 1)
	got, e = ComposeGroups(r, InstrumentGroups{}, nil)
	if e != nil || got.CashKnown.RatString() != "10" || got.Segments[0].Weight != nil {
		t.Fatal("negative cash hidden by netting", got, e)
	}
	if _, e = ComposeGroups(r, InstrumentGroups{}, []string{uuid.NewString()}); e == nil {
		t.Fatal("unknown group")
	}
}

func TestCompositionDimensionsKeepGroupSelectionAndCashSeparate(t *testing.T) {
	a, b, g := uuid.NewString(), uuid.NewString(), uuid.NewString()
	groups := InstrumentGroups{Groups: []InstrumentGroup{{g, "Strategy"}}, Memberships: []GroupMembership{{a, "ABC", g}}}
	r := Result{PortfolioValue: calc.PortfolioValue{Valuation: calc.Valuation{KnownValue: big.NewRat(100, 1), Complete: true, ComponentValues: map[string]*big.Rat{"a": big.NewRat(30, 1), "b": big.NewRat(60, 1), "cash": big.NewRat(10, 1)}}}, Components: map[string]ComponentIdentity{"a": {LedgerID: a, Kind: "position", Key: "ABC", Currency: "USD", InstrumentType: "share"}, "b": {LedgerID: b, Kind: "position", Key: "ABC", Currency: "USD", InstrumentType: "share"}, "cash": {LedgerID: a, Kind: "cash", Key: "RUB", Currency: "RUB"}}}
	for _, dimension := range []string{"account", "currency", "type"} {
		got, e := ComposeComposition(r, groups, []string{g}, dimension)
		if e != nil || got.SecuritiesKnown.RatString() != "30" || got.CashKnown.RatString() != "10" {
			t.Fatal(dimension, got, e)
		}
		expected := 2
		if dimension == "account" {
			expected = 3
		}
		if len(got.Segments) != expected {
			t.Fatal(dimension, got)
		}
		for _, s := range got.Segments {
			if s.Kind == "position" {
				if s.GroupID != "" || s.CategoryKey == "" {
					t.Fatal("ambiguous category", s)
				}
				if dimension != "account" && s.KnownValue.RatString() != "90" {
					t.Fatal(s)
				}
			}
		}
	}
	if _, e := ComposeComposition(r, groups, nil, "unsupported"); e == nil {
		t.Fatal("unknown breakdown")
	}
}
