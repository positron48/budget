package investmentvaluation

import (
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestConventionProfileBoundaries(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	v := PriceConventionPeriod{Scope: md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}, InstrumentID: tenant, From: at, To: at.Add(24 * time.Hour), Policy: SeriesPolicy{Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", MaxQuoteAge: time.Hour}}
	p := ConventionProfile{Key: "test", Title: "Reviewed test", Evidence: "Synthetic evidence", Enabled: true, Periods: map[string][]PriceConventionPeriod{"S": {v}}}
	if e := ValidateConventionProfile(tenant, p); e != nil {
		t.Fatal(e)
	}
	if _, ok := p.At("S", v.To); ok {
		t.Fatal("exclusive end accepted")
	}
	if _, ok := p.At("S", at); !ok {
		t.Fatal("start excluded")
	}
	for _, change := range []func(*PriceConventionPeriod){
		func(v *PriceConventionPeriod) { v.TenantID = "foreign" },
		func(v *PriceConventionPeriod) { v.From = v.From.Add(time.Nanosecond) },
		func(v *PriceConventionPeriod) { v.Policy.MaxQuoteAge = 0 },
		func(v *PriceConventionPeriod) { v.Policy.CurrentMaxQuoteAge = time.Hour },
		func(v *PriceConventionPeriod) { v.Policy.Kind = "daily_candle"; v.Policy.MaxQuoteAge = 0 },
	} {
		bad := v
		change(&bad)
		p.Periods["S"] = []PriceConventionPeriod{bad}
		if ValidateConventionProfile(tenant, p) == nil {
			t.Fatal("invalid convention accepted", bad)
		}
	}
	p.Periods["S"] = []PriceConventionPeriod{v, v}
	if ValidateConventionProfile(tenant, p) == nil {
		t.Fatal("overlap accepted")
	}
	c := &md.SessionCalendar{Scope: v.Scope, InstrumentID: v.InstrumentID, SourceKind: "exchange", ID: "calendar", Revision: 1, From: at, Through: at.Add(48 * time.Hour), Sessions: []md.TradingSession{{ObservationAt: at, CloseAt: at.Add(8 * time.Hour), AvailableAt: at.Add(9 * time.Hour)}}}
	v.Policy = SeriesPolicy{Kind: "daily_candle", SourceKind: "exchange", Adjustment: "unadjusted", Calendar: c}
	p.Periods["S"] = []PriceConventionPeriod{v}
	if e := ValidateConventionProfile(tenant, p); e != nil {
		t.Fatal(e)
	}
	v.Policy.CurrentMaxQuoteAge = time.Hour
	p.Periods["S"] = []PriceConventionPeriod{v}
	if e := ValidateConventionProfile(tenant, p); e != nil {
		t.Fatal(e)
	}
	for _, age := range []time.Duration{-time.Hour, 31 * 24 * time.Hour} {
		v.Policy.CurrentMaxQuoteAge = age
		p.Periods["S"] = []PriceConventionPeriod{v}
		if ValidateConventionProfile(tenant, p) == nil {
			t.Fatal("unbounded current quote SLA", age)
		}
	}
	v.Policy.CurrentMaxQuoteAge = time.Hour
	p.Periods["S"] = []PriceConventionPeriod{v}
	c.Sessions[0].AvailableAt = at.Add(time.Hour)
	if ValidateConventionProfile(tenant, p) == nil {
		t.Fatal("pre-close publication accepted")
	}
}

func TestRetrospectiveConventionAndCurrentSeparation(t *testing.T) {
	q := SeriesPolicy{Kind: "daily_candle", SourceKind: "exchange", Adjustment: "provider_unspecified", Retrospective: &md.RetrospectiveDailyPolicy{Convention: md.RetrospectiveCloseNextUTCDay, MaxAge: 96 * time.Hour}}
	tenant := "11111111-1111-4111-8111-111111111111"
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	p := ConventionProfile{Key: "retro", Title: "Retrospective", Evidence: "Explicit retrospective valuation rule", Enabled: true, Periods: map[string][]PriceConventionPeriod{"S": {{Scope: md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}, InstrumentID: tenant, From: at, To: at.Add(24 * time.Hour), Policy: q}}}}
	if err := ValidateConventionProfile(tenant, p); err != nil {
		t.Fatal(err)
	}
	if _, ok := q.CurrentPolicy(); ok {
		t.Fatal("daily close became a live quote")
	}
	q.CurrentMaxQuoteAge = time.Hour
	current, ok := q.CurrentPolicy()
	if !ok || current.Retrospective != nil || current.Calendar != nil || current.Kind != "last_price" || current.MaxQuoteAge != time.Hour {
		t.Fatal(current, ok)
	}
	p.Periods["S"][0].Policy.Retrospective.MaxAge = 0
	if ValidateConventionProfile(tenant, p) == nil {
		t.Fatal("unbounded retrospective profile accepted")
	}
}
