package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentTrackerHistory100000Operations500Instruments(t *testing.T) {
	testTrackerHistoryScale(t, 10000, 500, false)
}

func TestInvestmentTrackerHistory3000Operations100Instruments(t *testing.T) {
	testTrackerHistoryScale(t, 300, 100, true)
}

// The complete upper-bound traversal is a performance check. CI runs it
// separately without race instrumentation; quarterly exactness and the smaller
// seven-year fixture remain in the race-enabled financial suite.
func TestTrackerHistorySevenYearScale100000Operations500Instruments(t *testing.T) {
	testTrackerHistoryScale(t, 10000, 500, true)
}

func testTrackerHistoryScale(t *testing.T, operationsPerAccount, instruments int, fullRange bool) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	repo := NewMarketDataRepo(pool)
	start := time.Date(2013, 1, 1, 0, 0, 0, 0, time.UTC)
	from := start.AddDate(1, 0, 0)
	ids := []string{}
	values := make([][]any, 0, 10*operationsPerAccount)
	expected := make([]int64, 31)
	cutoffs := make([]time.Time, 31)
	for i := range cutoffs {
		cutoffs[i], _ = nav.DayCutoff(from.AddDate(0, 0, i).Format(time.DateOnly))
	}
	for account := 0; account < 10; account++ {
		l, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: fmt.Sprintf("Synthetic %d", account), StartedAt: start, PortfolioTracking: true}, fmt.Sprintf("tracker-%d", account))
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, l.ID)
		for i := 0; i < operationsPerAccount; i++ {
			at := start
			if i >= instruments {
				at = from.AddDate(0, 0, (i-instruments)%3650)
			}
			o := d.InvestmentOperation{ID: uuid.NewString(), TenantID: tenant, LedgerID: l.ID, Kind: "buy", InstrumentKey: fmt.Sprintf("S%d", i%instruments), Currency: "RUB", Amount: "10", Quantity: "1", OccurredAt: at, Sequence: int64(i + 1), Status: "posted", Version: 1}
			if e = inv.ValidateOperation(&o); e != nil {
				t.Fatal(e)
			}
			raw, e := json.Marshal(o)
			if e != nil {
				t.Fatal(e)
			}
			values = append(values, []any{o.ID, tenant, l.ID, o.Kind, o.OccurredAt, o.SettlementAt, o.Sequence, o.Version, o.Status, raw})
			for day := range expected {
				cut := cutoffs[day]
				if !at.After(cut) {
					expected[day]++
				}
			}
		}
	}
	// Synthetic fixtures bypass journal mutation cost so the measurement covers
	// the actual read over 100k validated facts, not 100k sequential imports.
	fixture, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = fixture.Rollback(ctx) }()
	// Invalidation correctness has a separate integration test. Avoid 100k
	// pointless updates of one generation row while constructing this fixture;
	// all keys, constraints and the production read path remain enabled.
	if _, e = fixture.Exec(ctx, `ALTER TABLE investment_operations DISABLE TRIGGER nav_generation_investment_operations`); e != nil {
		t.Fatal(e)
	}
	if _, e = fixture.CopyFrom(ctx, pgx.Identifier{"investment_operations"}, []string{"id", "tenant_id", "ledger_id", "kind", "occurred_at", "settlement_at", "sequence", "version", "status", "data"}, pgx.CopyFromRows(values)); e != nil {
		t.Fatal(e)
	}
	if _, e = fixture.Exec(ctx, `ALTER TABLE investment_operations ENABLE TRIGGER nav_generation_investment_operations`); e != nil {
		t.Fatal(e)
	}
	if e = fixture.Commit(ctx); e != nil {
		t.Fatal(e)
	}

	batch := HistoricalPriceBatch{Version: 1}
	listings := []trackerHistorySeries{}
	for i := 0; i < instruments; i++ {
		id := uuid.NewString()
		key := fmt.Sprintf("S%d", i)
		instrument := md.Instrument{ID: id, Ticker: key, Name: "Synthetic", Kind: "share", Currency: "RUB", Detailed: true}
		observations := []md.Observation{}
		for day := -5; day < 31; day++ {
			observations = append(observations, md.Observation{InstrumentID: id, Kind: "daily_candle", At: from.AddDate(0, 0, day), SourceKind: "exchange", Adjustment: "unadjusted", Currency: "RUB", PriceUnit: "currency_per_unit", Price: "10", Open: "10", High: "10", Low: "10", Volume: "1", Complete: true})
		}
		batch.Listings = append(batch.Listings, HistoricalPriceListing{Provider: "moex", AccessScope: "personal", Instrument: instrument, Observations: observations, ResponseHashes: []string{strings.Repeat("a", 64)}})
		periods := []md.MappingPeriod{{Provider: "moex", AccessScope: "personal", InstrumentID: id, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: start, Evidence: "Synthetic history"}}
		batch.Mappings = append(batch.Mappings, md.MappingChange{InstrumentKey: key, RequestKey: "scale-" + key, Reason: "Synthetic history", HistoricalPeriods: &periods})
		listings = append(listings, trackerHistorySeries{"moex", "personal", id})
	}
	if _, e := repo.ImportHistoricalPrices(ctx, tenant, actor, batch, HistoricalPricesSHA256(batch), true); e != nil {
		t.Fatal(e)
	}
	policy := md.FXPolicy{Key: "scale", Mode: "market", Source: "synthetic", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "One unit", ValidityConvention: "Explicit"}
	if _, e := repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(listings)
	plan, e := pool.DB.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+trackerHistorySeriesSQL, tenant, encoded, from.AddDate(0, 0, -5), from.AddDate(0, 0, 30))
	if e != nil {
		t.Fatal(e)
	}
	for plan.Next() {
		var line string
		if e = plan.Scan(&line); e != nil {
			t.Fatal(e)
		}
		t.Log(line)
	}
	if e = plan.Err(); e != nil {
		t.Fatal(e)
	}
	plan.Close()
	began := time.Now()
	var history nav.TrackerHistory
	pprof.Do(ctx, pprof.Labels("phase", "history_read"), func(ctx context.Context) {
		history, e = repo.TrackerHistory(ctx, tenant, actor, nav.TrackerHistoryRequest{LedgerIDs: ids, Currency: "RUB", FXPolicyKey: policy.Key, From: from.Format(time.DateOnly), To: "2024-01-08"})
	})
	elapsed := time.Since(began)
	if e != nil {
		t.Fatal(e)
	}
	if len(history.Points) != 31 || history.NextDate != "2014-02-01" {
		t.Fatal("ten-year pagination", len(history.Points), history.NextDate)
	}
	for i, point := range history.Points {
		want := big.NewRat(expected[i]*10, 1)
		if point.State != "ready" || point.Positions != instruments || point.PricedPositions != instruments || point.KnownValue.Cmp(want) != 0 {
			t.Fatal(i, point, want)
		}
	}
	t.Logf("%d operations, %d instruments, ten-year request, 31 valued days: %s", 10*operationsPerAccount, instruments, elapsed)
	began = time.Now()
	quarter, err := repo.TrackerHistory(ctx, tenant, actor, nav.TrackerHistoryRequest{CompactIssues: true, PageDays: 93, LedgerIDs: ids, Currency: "RUB", FXPolicyKey: policy.Key, From: from.Format(time.DateOnly), To: "2024-01-08"})
	quarterElapsed := time.Since(began)
	if err != nil || len(quarter.Points) != 93 {
		t.Fatal("quarter scale", len(quarter.Points), err)
	}
	began = time.Now()
	for part := 0; part < 3; part++ {
		legacy, err := (&MarketDataRepo{pool: pool}).TrackerHistory(ctx, tenant, actor, nav.TrackerHistoryRequest{LedgerIDs: ids, Currency: "RUB", FXPolicyKey: policy.Key, From: from.AddDate(0, 0, part*31).Format(time.DateOnly), To: "2024-01-08"})
		if err != nil || len(legacy.Points) != 31 || legacy.BasisKey != quarter.BasisKey {
			t.Fatal("legacy scale", err)
		}
		for i, got := range legacy.Points {
			want := quarter.Points[part*31+i]
			if got.Date != want.Date || got.State != want.State || got.CoverageKey != want.CoverageKey || got.KnownValue.Cmp(want.KnownValue) != 0 || got.PricedPositions != want.PricedPositions || got.Positions != want.Positions || len(got.Issues) != want.IssueCount {
				t.Fatal("quarter scale changed exact valuation", got.Date)
			}
		}
	}
	t.Logf("%d operations, %d instruments, 93 valued days: quarter=%s, three legacy reads=%s", 10*operationsPerAccount, instruments, quarterElapsed, time.Since(began))
	began = time.Now()
	warmed, err := repo.TrackerHistory(ctx, tenant, actor, nav.TrackerHistoryRequest{CompactIssues: true, PageDays: 93, LedgerIDs: ids, Currency: "RUB", FXPolicyKey: policy.Key, From: from.Format(time.DateOnly), To: "2024-01-08"})
	if err != nil || len(warmed.Points) != 93 || warmed.BasisKey != quarter.BasisKey {
		t.Fatal("warm exact history", err)
	}
	t.Logf("%d operations, %d instruments, warm 93-day projection with current source fence: %s", 10*operationsPerAccount, instruments, time.Since(began))

	if fullRange {
		// Measure the complete seven-year traversal as well as the individual page.
		// This fixture intentionally has missing later quotes: the traversal must
		// retain partial coverage rather than turn missing prices into zero profit.
		sevenYearRepo := NewMarketDataRepo(pool)
		sevenYearEnd := from.AddDate(7, 0, -1).Format(time.DateOnly)
		for _, pass := range []string{"cold", "warm"} {
			began = time.Now()
			pages, days := 0, 0
			next := from.Format(time.DateOnly)
			for next != "" {
				page, err := sevenYearRepo.TrackerHistory(ctx, tenant, actor, nav.TrackerHistoryRequest{CompactIssues: true, PageDays: 93, LedgerIDs: ids, Currency: "RUB", FXPolicyKey: policy.Key, From: next, To: sevenYearEnd})
				if err != nil || len(page.Points) == 0 || len(page.Points) > 93 {
					t.Fatal("seven-year traversal", pass, next, err)
				}
				for _, point := range page.Points {
					if point.Date != from.AddDate(0, 0, days).Format(time.DateOnly) {
						t.Fatal("seven-year date discontinuity", point.Date, days)
					}
					days++
				}
				pages++
				next = page.NextDate
			}
			if days != 2557 || pages != 28 {
				t.Fatal("seven-year traversal shape", days, pages)
			}
			t.Logf("%d operations, %d instruments, complete seven-year sequential traversal (%s), %d days / %d pages: %s", 10*operationsPerAccount, instruments, pass, days, pages, time.Since(began))
		}

	}

	for _, last := range []string{from.AddDate(0, 0, 30).Format(time.DateOnly), "2024-01-08"} {
		began = time.Now()
		h, err := repo.TrackerHistory(ctx, tenant, actor, nav.TrackerHistoryRequest{LedgerIDs: ids, Currency: "RUB", FXPolicyKey: policy.Key, From: from.Format(time.DateOnly), To: last, ContributionOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(h.Points) != 1 || h.Baseline == nil || len(h.Contributions) != 10*instruments {
			t.Fatal("attribution scale shape", len(h.Points), len(h.Contributions))
		}
		for _, r := range h.Contributions {
			if r.Profit == nil || r.Profit.Sign() != 0 {
				t.Fatal("flat-price attribution must retain the last known historical price", r)
			}
		}
		t.Logf("%d operations, %d instruments, contribution boundaries through %s: %s", 10*operationsPerAccount, instruments, last, time.Since(began))
	}
	began = time.Now()
	selection, err := repo.TrackerHistory(ctx, tenant, actor, nav.TrackerHistoryRequest{LedgerIDs: ids, Currency: "RUB", FXPolicyKey: policy.Key, From: from.Format(time.DateOnly), To: from.AddDate(0, 0, 30).Format(time.DateOnly), PerformanceFrom: from.Format(time.DateOnly), InstrumentKeys: []string{"S0"}})
	if err != nil || len(selection.Points) != 31 || selection.Baseline == nil {
		t.Fatal("selected scale history", err)
	}
	for _, point := range selection.Points {
		if point.Positions != 1 || point.PricedPositions != 1 || point.State != "ready" || point.FlowReason != "" || point.StrategyFlow == nil {
			t.Fatal("selection inherited other securities", point)
		}
	}
	t.Logf("%d operations, %d instruments, one-security 31-day signed scope: %s", 10*operationsPerAccount, instruments, time.Since(began))
	sparseDates, _ := json.Marshal([]time.Time{cutoffs[0].AddDate(0, 0, -1), cutoffs[30]})
	sparsePlan, err := pool.DB.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+trackerHistorySparseSeriesSQL, tenant, encoded, sparseDates)
	if err != nil {
		t.Fatal(err)
	}
	for sparsePlan.Next() {
		var line string
		if err = sparsePlan.Scan(&line); err != nil {
			t.Fatal(err)
		}
		t.Log(line)
	}
	if err = sparsePlan.Err(); err != nil {
		t.Fatal(err)
	}
	sparsePlan.Close()
}
