package postgres

import (
	"context"
	"math/big"
	"time"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

type trackerStrategyDay struct {
	value  *big.Rat
	reason string
	reset  bool
}

func trackerStrategyFlows(ctx context.Context, tenant string, ids []string, inputs map[string]investmentReplayInput, market map[string]trackerHistoryMarket, splits map[string]map[string][]d.InvestmentOperation, fixings []calc.FXFixing, policy, currency string, after, through, readAt time.Time) (map[string]trackerStrategyDay, error) {
	loc, e := time.LoadLocation("Europe/Moscow")
	if e != nil {
		return nil, e
	}
	ops := []d.InvestmentOperation{}
	for _, id := range ids {
		for _, op := range inputs[id].Operations {
			inWindow := op.OccurredAt.After(after) && !op.OccurredAt.After(through)
			if f := op.TradeFinancials; f != nil {
				inWindow = inWindow || f.Fee.At.After(after) && !f.Fee.At.After(through)
			}
			if inWindow {
				ops = append(ops, op)
			}
		}
	}
	flows, e := calc.TrackerFlows(ids, ops, func(op d.InvestmentOperation) (calc.SecurityFlowValue, error) {
		at, e := nav.DayCutoff(op.OccurredAt.In(loc).Format(time.DateOnly))
		if e != nil {
			return calc.SecurityFlowValue{}, e
		}
		quote, e := trackerHistoricalQuote(tenant, op.InstrumentKey, market[op.InstrumentKey], op.OccurredAt, readAt)
		if e != nil {
			return calc.SecurityFlowValue{}, e
		}
		if quote.CleanPrice == nil || quote.State != "fresh" {
			return calc.SecurityFlowValue{}, nil
		}
		price, e := nav.RebaseHistoricalPrice(quote.CleanPrice, quote.At, op.OccurredAt, op.InstrumentKey, splits[op.LedgerID][op.InstrumentKey])
		if e != nil {
			return calc.SecurityFlowValue{}, e
		}
		quantity, ok := new(big.Rat).SetString(op.Quantity)
		if !ok {
			return calc.SecurityFlowValue{}, d.ErrInvestmentInvalid
		}
		return calc.SecurityFlowValue{Value: new(big.Rat).Mul(price, quantity), Currency: quote.Currency, Date: at.In(loc).Format(time.DateOnly), SourceID: "reviewed_retrospective_transfer_close"}, nil
	})
	if e != nil {
		return nil, e
	}
	resolvers := map[string]*calc.FXResolver{}
	converted, e := calc.ConvertExternalFlows(flows, currency, loc, func(date string) (*calc.FXResolver, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if r := resolvers[date]; r != nil {
			return r, nil
		}
		at, e := nav.DayCutoff(date)
		if e != nil {
			return nil, e
		}
		r, e := calc.NewFXResolver(fixings, policy, at)
		if e == nil {
			resolvers[date] = r
		}
		return r, e
	})
	if e != nil {
		return nil, e
	}
	out := map[string]trackerStrategyDay{}
	for _, f := range converted {
		if !f.At.After(after) || f.At.After(through) {
			continue
		}
		day := out[f.Date]
		if day.value == nil {
			day.value = new(big.Rat)
		}
		if f.Kind == "opening_position" {
			day.reset = true
		}
		if f.Value == nil {
			day.reason = "strategy_flow_unknown"
		} else {
			day.value.Add(day.value, f.Value)
		}
		out[f.Date] = day
	}
	return out, nil
}
