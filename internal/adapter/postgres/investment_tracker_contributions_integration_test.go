package postgres

import (
	"context"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestInvestmentTrackerContributionsOwnFlowsGroupsAndClosedPositions(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledgers := []d.InvestmentLedger{}
	for _, name := range []string{"A", "B", "C"} {
		l, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: name, StartedAt: day.AddDate(-1, 0, 0), PortfolioTracking: true}, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		ledgers = append(ledgers, l)
	}
	versions := map[string]int64{}
	post := func(i int, key, kind, qty, amount, fee string, hours int) {
		l := ledgers[i]
		v := versions[l.ID] + 1
		_, e := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: v, Operation: d.InvestmentOperation{LedgerID: l.ID, InstrumentKey: key, Kind: kind, Quantity: qty, Amount: amount, Fee: fee, Currency: "RUB", OccurredAt: day.Add(time.Duration(hours) * time.Hour)}})
		if e != nil {
			t.Fatal(e)
		}
		versions[l.ID] = v
	}
	post(0, "S", "buy", "2", "160", "2", 1)
	post(0, "S", "sell", "1", "90", "0", 25)
	post(0, "S", "dividend", "", "5", "", 26)
	post(1, "S", "buy", "1", "100", "0", 1)
	post(2, "CLOSED", "buy", "1", "50", "0", 1)
	post(2, "CLOSED", "sell", "1", "70", "0", 25)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	firstInstrument := ""
	for _, key := range []string{"S", "CLOSED"} {
		i := md.Instrument{ID: uuid.NewString(), Ticker: key, Name: key, Kind: "share", Currency: "RUB"}
		if firstInstrument == "" {
			firstInstrument = i.ID
		}
		tx, e := pool.DB.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = saveMarketInstrument(ctx, tx, scope, i); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: key, RequestKey: key, Reason: "Synthetic", Periods: []md.MappingPeriod{{Provider: "tinvest", AccessScope: "personal", InstrumentID: i.ID, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: day.AddDate(-1, 0, 0), Evidence: "Synthetic"}}}); e != nil {
			t.Fatal(e)
		}
		for _, n := range []int{-2, -1, 1} {
			price := "100"
			if n == 1 {
				price = "120"
			}
			if key == "CLOSED" {
				price = "60"
			}
			o := md.Observation{InstrumentID: i.ID, Kind: "daily_candle", At: day.AddDate(0, 0, n), SourceKind: "exchange", Adjustment: "unadjusted", Currency: "RUB", PriceUnit: "currency_per_unit", Price: price, Open: price, High: price, Low: price, Volume: "1", Complete: true}
			if _, e = repo.SaveObservations(ctx, scope, []md.Observation{o}); e != nil {
				t.Fatal(e)
			}
		}
	}
	policy := md.FXPolicy{Key: "contribution-market", Mode: "market", Source: "synthetic", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "One unit", ValidityConvention: "Explicit"}
	if _, e := repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	a, b := uuid.NewString(), uuid.NewString()
	g := nav.InstrumentGroups{Groups: []nav.InstrumentGroup{{ID: a, Name: "Alpha"}, {ID: b, Name: "Beta"}}, Memberships: []nav.GroupMembership{{LedgerID: ledgers[0].ID, InstrumentKey: "S", GroupID: a}, {LedgerID: ledgers[1].ID, InstrumentKey: "S", GroupID: b}, {LedgerID: ledgers[2].ID, InstrumentKey: "CLOSED", GroupID: a}}}
	_, e := repo.SaveInstrumentGroups(ctx, tenant, actor, nav.SaveInstrumentGroups{InstrumentGroups: g, RequestKey: "groups", Reason: "Synthetic"})
	if e != nil {
		t.Fatal(e)
	}
	q := nav.TrackerHistoryRequest{LedgerIDs: []string{ledgers[0].ID, ledgers[1].ID, ledgers[2].ID}, Currency: "RUB", FXPolicyKey: policy.Key, From: "2024-01-01", To: "2024-01-03", ContributionOnly: true}
	read := func(want string, rows int) {
		h, e := repo.TrackerHistory(ctx, tenant, actor, q)
		if e != nil {
			t.Fatal(e)
		}
		if h.Baseline == nil || len(h.Points) != 1 || h.NextDate != "" || len(h.Contributions) != rows {
			t.Fatal("boundary shape", h)
		}
		sum := new(big.Rat)
		for _, r := range h.Contributions {
			if r.Profit == nil {
				t.Fatal("unexpected unknown", r)
			}
			if r.InScope {
				sum.Add(sum, r.Profit)
			}
		}
		if sum.RatString() != want {
			t.Fatal("wrong selected flows", sum, want)
		}
	}
	read("93", 3)
	q.GroupIDs = []string{a}
	read("73", 2)
	q.GroupIDs = []string{a, b, a}
	read("93", 3)
	q.From = "2024-01-02"
	q.GroupIDs = nil
	read("45", 3)
	q.GroupIDs = []string{uuid.NewString()}
	if _, e = repo.TrackerHistory(ctx, tenant, actor, q); e == nil {
		t.Fatal("unknown group broadened scope")
	}

	_, e = svc.Transfer(ctx, tenant, actor, inv.TransferMutation{Action: "create", RequestKey: "move", ExpectedFromVersion: versions[ledgers[0].ID] + 1, ExpectedToVersion: versions[ledgers[1].ID] + 1, Transfer: d.InvestmentTransfer{FromLedgerID: ledgers[0].ID, ToLedgerID: ledgers[1].ID, InstrumentKey: "S", Quantity: "1", Currency: "RUB", OccurredAt: day.Add(49 * time.Hour)}})
	if e != nil {
		t.Fatal(e)
	}
	q.From = "2024-01-01"
	q.GroupIDs = nil
	read("93", 3)
	q.GroupIDs = []string{a}
	read("73", 2)
	q.GroupIDs = []string{b}
	read("20", 1)

	q.ContributionOnly = false
	q.PerformanceFrom = q.From
	q.MonetaryOnly = true
	q.GroupIDs = []string{a}
	h, err := repo.TrackerHistory(ctx, tenant, actor, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Points) != 3 || h.Baseline == nil {
		t.Fatal("group signed history shape")
	}
	expectedValues := []string{"260", "100", "0"}
	expectedFlows := []string{"212", "-165", "-120"}
	days := []calc.PerformanceDay{{Date: h.Baseline.Date, NAV: h.Baseline.KnownValue, Complete: true, ExternalFlow: new(big.Rat)}}
	for i, p := range h.Points {
		if p.State != "ready" || p.KnownValue.RatString() != expectedValues[i] || p.StrategyFlow == nil || p.StrategyFlow.RatString() != expectedFlows[i] {
			t.Fatal("group inherited a parent-account or peer-group flow", i, p)
		}
		days = append(days, calc.PerformanceDay{Date: p.Date, NAV: p.KnownValue, ExternalFlow: p.StrategyFlow, Complete: p.State == "ready"})
	}
	analytics, err := calc.CalculateStrategyAnalytics(days, nil)
	if err != nil || analytics.Points[3].Profit.RatString() != "73" {
		t.Fatal(analytics, err)
	}
	q.GroupIDs = nil
	q.MonetaryOnly = false
	q.InstrumentKeys = []string{"S"}
	h, err = repo.TrackerHistory(ctx, tenant, actor, q)
	if err != nil {
		t.Fatal(err)
	}
	if h.Points[2].KnownValue.RatString() != "240" || h.Points[2].StrategyFlow.Sign() != 0 || h.Points[0].StrategyFlow.RatString() != "262" || h.Points[1].StrategyFlow.RatString() != "-95" {
		t.Fatal("instrument scope inherited other securities or internal transfers", h)
	}
	q.GroupIDs = []string{a}
	q.MonetaryOnly = true
	h, err = repo.TrackerHistory(ctx, tenant, actor, q)
	if err != nil {
		t.Fatal(err)
	}
	if h.Points[0].KnownValue.RatString() != "200" || h.Points[0].StrategyFlow.RatString() != "162" {
		t.Fatal("group/instrument intersection was broadened", h)
	}
	q.InstrumentKeys = []string{"not-owned"}
	if _, err = repo.TrackerHistory(ctx, tenant, actor, q); err == nil {
		t.Fatal("unowned selection was broadened")
	}
	// Real planner evidence for the exact sparse SQL shape; no data is changed.
	var plan string
	raw := `[{"provider":"tinvest","scope":"personal","id":"` + firstInstrument + `"}]`
	dates := `["2023-12-31T20:59:59Z","2024-01-03T20:59:59Z"]`
	if e = pool.DB.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+trackerHistorySparseSeriesSQL, tenant, raw, dates).Scan(&plan); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(plan, "Actual Rows") {
		t.Fatal("missing executed planner evidence")
	}
}
func TestTrackerContributionsDoNotAssignUnallocatedFeesToGroups(t *testing.T) {
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	q := nav.TrackerHistoryRequest{LedgerIDs: []string{"L"}, GroupIDs: []string{"group"}, Currency: "RUB"}
	input := map[string]investmentReplayInput{"L": {Ledger: d.InvestmentLedger{Name: "Account"}, Operations: []d.InvestmentOperation{{ID: "fee", LedgerID: "L", Kind: "fee", Status: "posted", Currency: "RUB", Amount: "3", OccurredAt: day}}}}
	boundary := nav.TrackerHistoryPoint{Components: map[string]nav.TrackerContributionValue{}}
	r, e := readTrackerContributions(context.Background(), "tenant", q, input, nil, nil, []calc.FXFixing{}, "p", time.Time{}, day.Add(time.Hour), day, &boundary, []nav.TrackerHistoryPoint{boundary}, &nav.InstrumentGroups{})
	if e != nil || len(r) != 1 || r[0].InScope || r[0].Profit == nil || r[0].Profit.RatString() != "-3" {
		t.Fatal(r, e)
	}
}
