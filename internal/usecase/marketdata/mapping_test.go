package marketdata

import (
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestMappingScheduleBoundaries(t *testing.T) {
	tenant := uuid.NewString()
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	p := MappingPeriod{Provider: "tinvest", AccessScope: "personal", InstrumentID: uuid.NewString(), Kind: "bond", Currency: "RUB", Unit: "percent_of_nominal", Nominal: "1000", NominalCurrency: "RUB", From: at, To: at.AddDate(1, 0, 0), Evidence: "Issuer terms"}
	c := MappingChange{InstrumentKey: "BOND", RequestKey: "confirm", Reason: "Reviewed listing and nominal", Periods: []MappingPeriod{p}}
	if e := ValidateMappingChange(tenant, c); e != nil {
		t.Fatal(e)
	}
	next := p
	next.From = p.To
	next.To = time.Time{}
	next.Nominal = "800"
	c.Periods = append(c.Periods, next)
	if e := ValidateMappingChange(tenant, c); e != nil {
		t.Fatal(e)
	}
	m := Mapping{InstrumentKey: c.InstrumentKey, Version: 2, Periods: c.Periods}
	terms, ok := m.TermsAt(next.From)
	if !ok || terms.Nominal != "800" || terms.Revision != 2 {
		t.Fatal(terms, ok)
	}
	if _, ok = m.TermsAt(at.Add(-time.Second)); ok {
		t.Fatal("mapped before first period")
	}
	for _, mutate := range []func(*MappingChange){
		func(v *MappingChange) { v.Periods[1].From = at },
		func(v *MappingChange) { v.Periods[0].To = time.Time{} },
		func(v *MappingChange) { v.Periods[0].Nominal = "0" },
		func(v *MappingChange) { v.Periods[0].From = at.Add(time.Nanosecond) },
		func(v *MappingChange) { v.Periods[0].Evidence = "" },
		func(v *MappingChange) { v.Periods[0].Unit = "lots" },
	} {
		copy := c
		copy.Periods = append([]MappingPeriod{}, c.Periods...)
		mutate(&copy)
		if e := ValidateMappingChange(tenant, copy); e == nil {
			t.Fatal("invalid schedule accepted", copy)
		}
	}
	c.Periods[1].From = next.From.AddDate(0, 0, 1)
	m.Periods = c.Periods
	if _, ok = m.TermsAt(next.From); ok {
		t.Fatal("gap silently filled")
	}
}

func TestHistoricalScheduleDoesNotReplaceLiveTermsOrFillGaps(t *testing.T) {
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	live := MappingPeriod{Provider: "tinvest", AccessScope: "personal", InstrumentID: uuid.NewString(), Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: day, Evidence: "Confirmed current listing"}
	history := live
	history.Provider, history.InstrumentID, history.From = "twelvedata", uuid.NewString(), day.AddDate(0, 0, 2)
	m := Mapping{InstrumentKey: "S", Version: 1, Periods: []MappingPeriod{live}, HistoricalPeriods: []MappingPeriod{history}}
	if _, ok := m.HistoricalTermsAt(day); ok {
		t.Fatal("historical gap filled by live listing")
	}
	terms, ok := m.TermsAt(history.From)
	if !ok || terms.InstrumentID != live.InstrumentID {
		t.Fatal("history replaced live terms", terms)
	}
	terms, ok = m.HistoricalTermsAt(history.From)
	if !ok || terms.InstrumentID != history.InstrumentID {
		t.Fatal("native history terms missing", terms)
	}
	change := MappingChange{InstrumentKey: "S", RequestKey: "reviewed", Reason: "Confirmed history", Periods: m.Periods, HistoricalPeriods: &m.HistoricalPeriods}
	if err := ValidateMappingChange(uuid.NewString(), change); err != nil {
		t.Fatal(err)
	}
	m.HistoricalPeriods = append(m.HistoricalPeriods, history)
	if err := ValidateMappingChange(uuid.NewString(), change); err == nil {
		t.Fatal("overlapping history accepted")
	}
}
