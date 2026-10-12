package investmentvaluation

import (
	"math/big"
	"sort"
	"time"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type InstrumentFact struct {
	Terms   calc.PriceTerms
	Price   *calc.PriceFact
	Accrued *calc.AccruedFact
}
type PositionPayment struct {
	LedgerID, InstrumentKey, Currency string
	Gross, Tax, Net                   *big.Rat
}
type PositionView struct {
	HistoricalCost                                  LotCostSummary
	LedgerID, LedgerName, GroupID                   string
	Position                                        d.InvestmentPosition
	Value, Weight, UnrealizedClean, UnrealizedDirty *big.Rat
	NativePrice                                     calc.NormalizedPrice // per security, never per broker lot
	Facts                                           *InstrumentFact
	Payments                                        []PositionPayment
	Issues                                          []calc.QualityIssue
}
type PositionDetails struct {
	Date, Currency, FXPolicyKey, ValuationState string
	Classification                              InstrumentGroups
	Rows                                        []PositionView
	Metadata                                    map[string]md.Instrument
}

func positionNumber(v string) *big.Rat {
	if v == "" {
		return nil
	}
	r, ok := new(big.Rat).SetString(v)
	if !ok {
		return nil
	}
	return r
}
func difference(a *big.Rat, values ...*big.Rat) *big.Rat {
	if a == nil {
		return nil
	}
	out := new(big.Rat).Set(a)
	for _, v := range values {
		if v == nil {
			return nil
		}
		out.Sub(out, v)
	}
	return out
}

// PositionRows never converts native FIFO cost with the current FX rate. Native
// book figures and the reporting-currency market value are separate fields.
func PositionRows(snapshots []d.InvestmentSnapshot, value *Result, groups InstrumentGroups, payments []PositionPayment, selected []string) ([]PositionView, error) {
	selected, e := NormalizeGroupSelection(selected, groups)
	if e != nil {
		return nil, e
	}
	memberships := map[string]string{}
	for _, m := range groups.Memberships {
		memberships[m.LedgerID+"\x00"+m.InstrumentKey] = m.GroupID
	}
	wanted := map[string]bool{}
	for _, id := range selected {
		wanted[id] = true
	}
	if value != nil && value.Components != nil && value.ComponentValues != nil {
		if len(value.Components) != len(value.ComponentValues) {
			return nil, d.ErrInvestmentPrecondition
		}
		sum := new(big.Rat)
		for id, v := range value.ComponentValues {
			if _, exists := value.Components[id]; !exists {
				return nil, d.ErrInvestmentPrecondition
			}
			if v != nil {
				sum.Add(sum, v)
			}
		}
		if value.KnownValue == nil || sum.Cmp(value.KnownValue) != 0 {
			return nil, d.ErrInvestmentPrecondition
		}
	}
	weights := value != nil && value.Complete && value.KnownValue != nil && value.KnownValue.Sign() > 0 && value.ComponentValues != nil
	if weights {
		for _, v := range value.ComponentValues {
			if v == nil || v.Sign() < 0 {
				weights = false
			}
		}
	}
	paymentsByPosition := map[string][]PositionPayment{}
	for _, p := range payments {
		k := p.LedgerID + "\x00" + p.InstrumentKey
		paymentsByPosition[k] = append(paymentsByPosition[k], p)
	}
	out := []PositionView{}
	for _, s := range snapshots {
		if value != nil && (s.Ledger.TenantID != value.TenantID || !s.AsOf.Equal(value.At)) {
			return nil, d.ErrInvestmentPrecondition
		}
		for _, p := range s.Positions {
			group := memberships[s.Ledger.ID+"\x00"+p.InstrumentKey]
			if len(wanted) > 0 && !wanted[group] {
				continue
			}
			p.Lots = nil // aggregate view does not duplicate all FIFO lots
			row := PositionView{LedgerID: s.Ledger.ID, LedgerName: s.Ledger.Name, GroupID: group, Position: p, Issues: []calc.QualityIssue{}}
			row.Payments = paymentsByPosition[row.LedgerID+"\x00"+p.InstrumentKey]
			quantity := positionNumber(p.Quantity)
			if quantity == nil || quantity.Sign() < 0 {
				return nil, d.ErrInvestmentInvalid
			}
			id := componentID(row.LedgerID, "position", p.InstrumentKey)
			if value == nil || value.Components == nil || value.ComponentValues == nil {
				row.Issues = append(row.Issues, calc.QualityIssue{ComponentID: id, Code: "valuation_unavailable"})
			} else {
				identity, exists := value.Components[id]
				if (!exists && quantity.Sign() != 0) || exists && (positionNumber(identity.Quantity) == nil || positionNumber(identity.Quantity).Cmp(quantity) != 0) {
					return nil, d.ErrInvestmentPrecondition
				}
				amount := value.ComponentValues[id]
				if !exists && quantity.Sign() == 0 {
					amount = new(big.Rat)
				}
				if amount != nil {
					row.Value = new(big.Rat).Set(amount)
					if weights {
						row.Weight = new(big.Rat).Quo(amount, value.KnownValue)
					}
				}
				if fact, exists := value.InstrumentFacts[p.InstrumentKey]; exists {
					copy := fact
					row.Facts = &copy
				}
				native, exists := value.Positions[id]
				if exists {
					if native.Currency == p.Currency {
						row.UnrealizedClean = difference(native.Clean, positionNumber(p.CleanCost))
						row.UnrealizedDirty = difference(native.Dirty, positionNumber(p.CleanCost), positionNumber(p.AcquiredAccrued))
					} else {
						row.Issues = append(row.Issues, calc.QualityIssue{ComponentID: id, Code: "book_price_currency_mismatch"})
					}
					row.NativePrice = native
					if quantity.Sign() > 0 {
						divide := func(v *big.Rat) *big.Rat {
							if v == nil {
								return nil
							}
							return new(big.Rat).Quo(v, quantity)
						}
						row.NativePrice.Clean = divide(native.Clean)
						row.NativePrice.Accrued = divide(native.Accrued)
						row.NativePrice.Dirty = divide(native.Dirty)
					}
				}
				for _, issue := range append(append([]calc.QualityIssue{}, value.Issues...), value.Warnings...) {
					if issue.ComponentID == id || issue.ComponentID == row.LedgerID {
						row.Issues = append(row.Issues, issue)
					}
				}
			}
			if quantity.Sign() == 0 {
				row.UnrealizedClean = difference(new(big.Rat), positionNumber(p.CleanCost))
				row.UnrealizedDirty = difference(new(big.Rat), positionNumber(p.CleanCost), positionNumber(p.AcquiredAccrued))
			}
			if len(out) >= 10000 {
				return nil, d.ErrInvestmentLimit
			}
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Position.InstrumentKey != b.Position.InstrumentKey {
			return a.Position.InstrumentKey < b.Position.InstrumentKey
		}
		return a.LedgerID < b.LedgerID
	})
	return out, nil
}

type InstrumentDetails struct {
	PositionDetails
	InstrumentKey string
	Operations    []d.InvestmentOperation
	NextCursor    string
	AsOf          time.Time
}

type InstrumentPriceDay struct {
	AdjustmentProof                                             *calc.PriceAdjustmentProof `json:",omitempty"`
	PriceEvidence                                               string                     `json:",omitempty"`
	Accrued                                                     *calc.AccruedFact
	Date, State, NativeCurrency, PriceSource, Adjustment        string
	PriceAt                                                     time.Time
	Percent, NativeClean, NativeDirty, TargetClean, TargetDirty *big.Rat
	Issues                                                      []calc.QualityIssue
}
type InstrumentPriceHistory struct {
	Markers                              []d.InvestmentOperation
	InstrumentKey, Currency, FXPolicyKey string
	PriceProfileVersion                  int64
	Days                                 []InstrumentPriceDay
}
