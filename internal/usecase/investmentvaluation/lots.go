package investmentvaluation

import (
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"time"
)

// LotView carries exact remaining values, including unknown costs. Origin is
// the original confirmed acquisition, even after an inter-instrument event.
type LotView struct {
	LedgerID, LedgerName, Currency string
	Lot                            inv.RemainingLot
	Origin                         d.InvestmentOperation
	HistoricalCost                 LotCostSummary
	FX                             *calc.ResolvedFX
	FXAt                           time.Time
	Changes                        []inv.LotChange
}
type LotDetails struct {
	Date, Currency, FXPolicyKey, NextCursor string
	Total                                   int
	Rows                                    []LotView
}

// AcquisitionFX returns the same resolver evidence used by Convert. Identity
// and known-zero costs need no market observation; unknown costs remain unknown.
func (c *LotCostConverter) AcquisitionFX(lot inv.RemainingLot, from, to string) (*calc.ResolvedFX, time.Time, error) {
	if from == to || lot.AcquiredAt.IsZero() || (lot.CleanCost == nil || lot.CleanCost.Sign() == 0) && (lot.AcquiredAccrued == nil || lot.AcquiredAccrued.Sign() == 0) {
		return nil, time.Time{}, nil
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	at, err := DayCutoff(lot.AcquiredAt.In(loc).Format(time.DateOnly))
	if err != nil {
		return nil, time.Time{}, err
	}
	resolver, err := c.resolver(at)
	if err != nil {
		return nil, at, err
	}
	rate, ok := resolver.Resolve(from, to)
	if !ok {
		return nil, at, nil
	}
	return &rate, at, nil
}
func (c *LotCostConverter) resolver(at time.Time) (*calc.FXResolver, error) {
	key := at.Format(time.RFC3339Nano)
	if c.cache == nil {
		c.cache = map[string]*calc.FXResolver{}
	}
	if cached := c.cache[key]; cached != nil {
		return cached, nil
	}
	r, e := calc.NewFXResolver(c.Fixings, c.Policy, at)
	if e == nil {
		c.cache[key] = r
	}
	return r, e
}
