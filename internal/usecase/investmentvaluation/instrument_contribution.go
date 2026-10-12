package investmentvaluation

import (
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"math/big"
	"sort"
)

// Contribution is economic P&L, not return on a hypothetical cost denominator.
// Opening market is negative, closing market and signed cash movements positive.
type InstrumentContribution struct {
	Metadata      md.Instrument
	InstrumentKey string
	LedgerIDs     []string
	Closed        bool
	Known, Share  *big.Rat
	Complete      bool
	Components    []calc.ProfitComponent
}
type InstrumentContributions struct {
	Rows           []InstrumentContribution
	RemainderShare *big.Rat
	Remainder      calc.ProfitComponent
	Complete       bool
}
type ContributionBuilder struct {
	incomplete bool
	rows       map[string]*InstrumentContribution
	ledgers    map[string]map[string]bool
	counts     map[string]map[string]int
}

func NewContributionBuilder() *ContributionBuilder {
	return &ContributionBuilder{rows: map[string]*InstrumentContribution{}, ledgers: map[string]map[string]bool{}, counts: map[string]map[string]int{}}
}
func (b *ContributionBuilder) Ensure(key, ledger string, open bool) {
	row := b.rows[key]
	if row == nil {
		row = &InstrumentContribution{InstrumentKey: key, Closed: true, Complete: true}
		for _, kind := range []string{"opening_market", "closing_market", "cash_movements", "noncash_transfers"} {
			row.Components = append(row.Components, calc.ProfitComponent{Kind: kind, Known: new(big.Rat), Complete: true})
		}
		b.rows[key] = row
		b.ledgers[key] = map[string]bool{}
		b.counts[key] = map[string]int{}
	}
	b.ledgers[key][ledger] = true
	if open {
		row.Closed = false
	}
}
func (b *ContributionBuilder) Add(key, ledger, kind string, amount *big.Rat, reason string) {
	b.Ensure(key, ledger, false)
	row := b.rows[key]
	for i := range row.Components {
		c := &row.Components[i]
		if c.Kind != kind {
			continue
		}
		if amount != nil {
			c.Known.Add(c.Known, amount)
			b.counts[key][kind]++
		}
		if reason != "" || amount == nil {
			c.Complete = false
			if c.Reason == "" {
				c.Reason = reason
				if c.Reason == "" {
					c.Reason = "component_value_unknown"
				}
			}
		}
		return
	}
	panic("invalid instrument contribution component")
}
func (b *ContributionBuilder) Finish(total *big.Rat) InstrumentContributions {
	out := InstrumentContributions{Complete: total != nil && !b.incomplete, Remainder: calc.ProfitComponent{Kind: "cash_and_unallocated", Complete: total != nil}}
	sum := new(big.Rat)
	keys := []string{}
	for key := range b.rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		source := b.rows[key]
		row := *source
		row.Components = append([]calc.ProfitComponent(nil), source.Components...)
		row.Known = new(big.Rat)
		known := false
		for i := range row.Components {
			c := &row.Components[i]
			if !c.Complete && b.counts[key][c.Kind] == 0 {
				c.Known = nil
			}
			if c.Known != nil {
				row.Known.Add(row.Known, c.Known)
				known = true
			}
			row.Complete = row.Complete && c.Complete && c.Known != nil
		}
		if !known {
			row.Known = nil
		}
		for ledger := range b.ledgers[key] {
			row.LedgerIDs = append(row.LedgerIDs, ledger)
		}
		sort.Strings(row.LedgerIDs)
		out.Complete = out.Complete && row.Complete
		if row.Known != nil {
			sum.Add(sum, row.Known)
		}
		out.Rows = append(out.Rows, row)
	}
	out.Remainder.Complete = out.Complete
	if out.Complete {
		out.Remainder.Known = new(big.Rat).Sub(total, sum)
		if total.Sign() != 0 {
			out.RemainderShare = new(big.Rat).Quo(out.Remainder.Known, total)
			for i := range out.Rows {
				out.Rows[i].Share = new(big.Rat).Quo(out.Rows[i].Known, total)
			}
		}
	} else {
		out.Remainder.Reason = "instrument_contribution_incomplete"
	}
	return out
}

// ApplyDistribution allocates the reviewed total fair value, already converted
// at the event's dated FX, equally and oppositely between source and recipient.
// It never adds to aggregate profit or cash. Missing evidence remains unknown;
// the FIFO cost ratio is deliberately not used as a market-value substitute.
func (b *ContributionBuilder) ApplyDistribution(op d.InvestmentOperation, value *big.Rat, reason string) {
	if op.Kind != "distribution" || op.Distribution == nil {
		return
	}
	if op.Distribution.MarketValue == "" || op.Distribution.MarketValueEvidence == "" {
		value, reason = nil, "noncash_allocation_required"
	}
	if value != nil && value.Sign() < 0 {
		value, reason = nil, "noncash_allocation_invalid"
	}
	b.Add(op.InstrumentKey, op.LedgerID, "noncash_transfers", value, reason)
	var received *big.Rat
	if value != nil {
		received = new(big.Rat).Neg(value)
	}
	b.Add(op.Distribution.TargetInstrumentKey, op.LedgerID, "noncash_transfers", received, reason)
}

func (b *ContributionBuilder) RestrictOperation(op d.InvestmentOperation) {
	if op.TaxReviewRequired && op.InstrumentKey == "" {
		b.incomplete = true
	}
	if op.TaxReviewRequired && op.InstrumentKey != "" {
		b.Add(op.InstrumentKey, op.LedgerID, "cash_movements", new(big.Rat), "tax_review_required")
	}
}
