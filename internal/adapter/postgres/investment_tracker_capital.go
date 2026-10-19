package postgres

import (
	"context"
	"math/big"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

// Opening capital comes from the recorded journal, not the market value on an
// arbitrary chart boundary. Only custody movements crossing this selection
// require old market quotes; ordinary buys and payouts use their original FX.
func trackerOpeningCapital(ctx context.Context, tx pgx.Tx, tenant string, q nav.TrackerHistoryRequest, inputs map[string]investmentReplayInput, market map[string]trackerHistoryMarket, splits map[string]map[string][]d.InvestmentOperation, fixings []calc.FXFixing, policy string, through, readAt time.Time, selected func(string, string) bool) (*big.Rat, string, []string, error) {
	ids := map[string]bool{}
	for _, id := range q.LedgerIDs {
		ids[id] = true
	}
	dates := map[string]time.Time{}
	loc, _ := time.LoadLocation("Europe/Moscow")
	for id, input := range inputs {
		for _, op := range input.Operations {
			if op.Status != "posted" || op.OccurredAt.After(through) || !selected(id, op.InstrumentKey) || (op.Kind != "security_transfer_in" && op.Kind != "security_transfer_out") || ids[op.CounterpartyLedgerID] && selected(op.CounterpartyLedgerID, op.InstrumentKey) {
				continue
			}
			date := op.OccurredAt.In(loc).Format(time.DateOnly)
			at, _ := nav.DayCutoff(date)
			dates[date] = at
		}
	}
	if len(dates) > 0 {
		sparse := []time.Time{}
		for _, at := range dates {
			sparse = append(sparse, at)
		}
		keys := []string{}
		for key := range market {
			keys = append(keys, key)
		}
		var err error
		market, err = readTrackerHistoryMarket(ctx, tx, tenant, keys, through, through, readAt, sparse...)
		if err != nil {
			return nil, "", nil, err
		}
	}
	var flows map[string]trackerStrategyDay
	var err error
	if len(q.GroupIDs) > 0 || len(q.InstrumentKeys) > 0 {
		flows, err = trackerSelectionFlows(ctx, tenant, q, inputs, market, splits, fixings, policy, time.Time{}, through, readAt, selected)
	} else {
		flows, err = trackerStrategyFlows(ctx, tenant, q.LedgerIDs, inputs, market, splits, fixings, policy, q.Currency, time.Time{}, through, readAt)
	}
	if err != nil {
		return nil, "", nil, err
	}
	capital := new(big.Rat)
	reason := ""
	excluded := []string{}
	for _, flow := range flows {
		if flow.reason != "" {
			reason = "historical_capital_incomplete"
			excluded = append(excluded, flow.excludedKeys...)
		}
		if flow.value != nil {
			capital.Add(capital, flow.value)
		}
	}
	slices.Sort(excluded)
	excluded = slices.Compact(excluded)
	if reason != "" {
		return nil, reason, excluded, nil
	}
	return capital, "", nil, nil
}
