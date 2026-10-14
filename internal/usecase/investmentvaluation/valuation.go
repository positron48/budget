// Package investmentvaluation composes confirmed journal snapshots and market
// evidence. It does not fetch data or imply that imported history is reconciled.
package investmentvaluation

import (
	"fmt"
	"math/big"
	"sort"
	"time"
	_ "time/tzdata"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type InstrumentEvidence struct {
	TenantID string
	Mapping  md.Mapping
	Price    md.SelectedPrice
	Accrued  *calc.AccruedFact
}
type PositionProvenance struct {
	AdjustmentProof     *calc.PriceAdjustmentProof `json:",omitempty"`
	TimeConvention      string                     `json:",omitempty"`
	Accrued             *calc.AccruedFact          `json:",omitempty"`
	MappingVersion      int64
	ObservationID       string
	ObservationRevision int64
	CalendarID          string
	CalendarRevision    int64
}
type ComponentIdentity struct{ LedgerID, Kind, Key, Currency, Quantity, InstrumentType string }
type Result struct {
	// Transient last-price valuation cannot be published as a closed daily point.
	Provisional       bool `json:",omitempty"`
	InstrumentFacts   map[string]InstrumentFact
	Components        map[string]ComponentIdentity
	DataGeneration    int64
	ConventionKey     string `json:",omitempty"`
	ConventionVersion int64  `json:",omitempty"`
	calc.PortfolioValue
	TenantID, Currency, FXPolicy string
	At                           time.Time
	LedgerVersions               map[string]int64
	Evidence                     map[string]PositionProvenance
	// These affect performance/history interpretation, not current quantities or
	// economic cash. Unknown future issue codes are conservatively blocking.
	Warnings []calc.QualityIssue
}

// DayCutoff uses the specified reporting timezone, including its historical
// offsets, and the database's microsecond precision. US close is not moved back
// into a Moscow date merely because its provider session has that date.
func DayCutoff(day string) (time.Time, error) {
	loc, e := time.LoadLocation("Europe/Moscow")
	if e != nil {
		return time.Time{}, e
	}
	date, e := time.ParseInLocation("2006-01-02", day, loc)
	if e != nil || date.Format("2006-01-02") != day {
		return time.Time{}, fmt.Errorf("invalid reporting date")
	}
	return date.AddDate(0, 0, 1).Add(-time.Microsecond).UTC(), nil
}
func componentID(ledger, kind, key string) string {
	return fmt.Sprintf("%d:%s/%s/%s", len(ledger), ledger, kind, key)
}

// Value assembles one or more accounts at exactly one cutoff. Each cash bucket
// enters once; cost basis and historic acquired interest never enter NAV.
func Value(tenant string, snapshots []d.InvestmentSnapshot, evidence map[string]InstrumentEvidence, fx []calc.FXFixing, target, policy string) (Result, error) {
	out := Result{InstrumentFacts: map[string]InstrumentFact{}, Components: map[string]ComponentIdentity{}, TenantID: tenant, Currency: target, FXPolicy: policy, LedgerVersions: map[string]int64{}, Evidence: map[string]PositionProvenance{}, Warnings: []calc.QualityIssue{}}
	if tenant == "" || len(snapshots) == 0 {
		return out, fmt.Errorf("tenant and snapshots required")
	}
	out.At = snapshots[0].AsOf
	var positions []calc.PositionValueInput
	var cash []calc.CashValueInput
	issues := []calc.QualityIssue{}
	for _, s := range snapshots {
		l := s.Ledger
		if l.TenantID != tenant || l.ID == "" || l.Version < 1 || out.LedgerVersions[l.ID] != 0 || s.AsOf.IsZero() || !s.AsOf.Equal(out.At) || l.StartedAt.IsZero() {
			return out, fmt.Errorf("incompatible ledger snapshot")
		}
		out.LedgerVersions[l.ID] = l.Version
		if s.AsOf.Before(l.StartedAt) {
			if len(s.Cash) != 0 || len(s.Positions) != 0 {
				return out, fmt.Errorf("positions before journal coverage")
			}
			issues = append(issues, calc.QualityIssue{ComponentID: l.ID, Code: "ledger_history_missing"})
			continue
		}
		for _, code := range s.Issues {
			issue := calc.QualityIssue{ComponentID: l.ID, Code: code}
			switch code {
			case "mixed_native_cost_currencies", "imported_cash_deficit_reviewed", "unknown_acquisition_cost", "unknown_acquired_interest", "historical_acquisition_date_unknown", "coupon_allocation_required", "history_before_opening_unknown", "settlement_assumed_trade_time":
				out.Warnings = append(out.Warnings, issue)
			default:
				issues = append(issues, issue)
			}
		}
		for _, c := range s.Cash {
			out.Components[componentID(l.ID, "cash", c.Currency)] = ComponentIdentity{LedgerID: l.ID, Kind: "cash", Key: c.Currency, Currency: c.Currency}
			cash = append(cash, calc.CashValueInput{ID: componentID(l.ID, "cash", c.Currency), Currency: c.Currency, Cash: calc.Cash{Settled: c.Settled, Receivable: c.Receivable, Payable: c.Payable}, Known: true})
		}
		for _, p := range s.Positions {
			id := componentID(l.ID, "position", p.InstrumentKey)
			out.Components[id] = ComponentIdentity{LedgerID: l.ID, Kind: "position", Key: p.InstrumentKey, Currency: p.Currency, Quantity: p.Quantity}
			input := calc.PositionValueInput{ID: id, Currency: p.Currency, Quantity: p.Quantity}
			quantity, ok := new(big.Rat).SetString(p.Quantity)
			if !ok || quantity.Sign() < 0 {
				return out, fmt.Errorf("invalid snapshot quantity")
			}
			if quantity.Sign() == 0 {
				positions = append(positions, input)
				continue
			}
			ev, ok := evidence[p.InstrumentKey]
			if !ok {
				issues = append(issues, calc.QualityIssue{ComponentID: id, Code: "mapping_missing"})
				positions = append(positions, input)
				continue
			}
			if ev.TenantID != tenant || ev.Mapping.InstrumentKey != p.InstrumentKey || ev.Mapping.Version < 1 {
				return out, fmt.Errorf("foreign instrument evidence")
			}
			out.Evidence[id] = PositionProvenance{MappingVersion: ev.Mapping.Version}
			terms, ok := ev.Mapping.TermsAt(out.At)
			if !ok {
				issues = append(issues, calc.QualityIssue{ComponentID: id, Code: "mapping_period_missing"})
				positions = append(positions, input)
				continue
			}
			fact := InstrumentFact{Terms: terms, Accrued: ev.Accrued}
			if ev.Price.State == "fresh" && len(ev.Price.Issues) == 0 {
				fact.Price = ev.Price.Fact
			}
			out.InstrumentFacts[p.InstrumentKey] = fact
			input.Currency = terms.Currency
			identity := out.Components[id]
			identity.Currency = terms.Currency
			identity.InstrumentType = terms.Kind
			out.Components[id] = identity
			input.Terms = &terms
			input.Accrued = ev.Accrued
			provenance := PositionProvenance{AdjustmentProof: ev.Price.AdjustmentProof, TimeConvention: ev.Price.TimeConvention, Accrued: ev.Accrued, MappingVersion: ev.Mapping.Version, ObservationID: ev.Price.ObservationID, ObservationRevision: ev.Price.ObservationRevision, CalendarID: ev.Price.CalendarID, CalendarRevision: ev.Price.CalendarRevision}
			if ev.Price.AdjustmentProof != nil {
				code := "reconstructed_price_valuation"
				if ev.Price.AdjustmentProof.Basis == NativeBondPercentBasis {
					code = "reviewed_native_bond_valuation"
				}
				out.Warnings = append(out.Warnings, calc.QualityIssue{ComponentID: id, Code: code})
			}
			if ev.Price.TimeConvention == md.RetrospectiveCloseNextUTCDay {
				out.Warnings = append(out.Warnings, calc.QualityIssue{ComponentID: id, Code: "retrospective_price_valuation"})
			}
			out.Evidence[id] = provenance
			if ev.Price.State == "fresh" && len(ev.Price.Issues) == 0 && ev.Price.Fact != nil {
				if provenance.ObservationID == "" || provenance.ObservationRevision < 1 {
					return out, fmt.Errorf("price provenance required")
				}
				input.Quote = ev.Price.Fact
			} else {
				for _, code := range ev.Price.Issues {
					issues = append(issues, calc.QualityIssue{ComponentID: id, Code: code})
				}
				if len(ev.Price.Issues) == 0 {
					issues = append(issues, calc.QualityIssue{ComponentID: id, Code: "price_unavailable"})
				}
			}
			// The journal's confirmed principal events independently constrain nominal.
			if terms.Kind == "bond" && terms.Unit == "percent_of_nominal" && p.OutstandingNominal != "" {
				journal, jok := new(big.Rat).SetString(p.OutstandingNominal)
				market, mok := new(big.Rat).SetString(terms.Nominal)
				if !jok || !mok {
					return out, fmt.Errorf("invalid outstanding nominal")
				}
				if journal.Cmp(market) != 0 {
					input.Quote = nil
					issues = append(issues, calc.QualityIssue{ComponentID: id, Code: "journal_nominal_conflict"})
				}
			}
			positions = append(positions, input)
		}
	}
	value, e := calc.ValuePortfolio(positions, cash, fx, target, policy, out.At)
	if e != nil {
		return out, e
	}
	out.PortfolioValue = value
	// Keep precise blocking causes and eliminate repetitive generic missing-price
	// diagnostics when the upstream source already explains the missing input.
	explained := map[string]bool{}
	for _, i := range issues {
		explained[i.ComponentID] = true
	}
	for _, i := range value.Issues {
		if !explained[i.ComponentID] || (i.Code != "quote_missing" && i.Code != "terms_missing") {
			issues = append(issues, i)
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].ComponentID != issues[j].ComponentID {
			return issues[i].ComponentID < issues[j].ComponentID
		}
		return issues[i].Code < issues[j].Code
	})
	out.Issues = []calc.QualityIssue{}
	for _, i := range issues {
		if len(out.Issues) == 0 || out.Issues[len(out.Issues)-1] != i {
			out.Issues = append(out.Issues, i)
		}
	}
	out.Complete = value.Complete && len(out.Issues) == 0
	return out, nil
}
