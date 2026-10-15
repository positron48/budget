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
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	repo := NewMarketDataRepo(pool)
	start := time.Date(2013, 1, 1, 0, 0, 0, 0, time.UTC)
	from := start.AddDate(1, 0, 0)
	ids := []string{}
	values := make([][]any, 0, 100000)
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
		for i := 0; i < 10000; i++ {
			at := start
			if i >= 500 {
				at = from.AddDate(0, 0, (i-500)%3650)
			}
			o := d.InvestmentOperation{ID: uuid.NewString(), TenantID: tenant, LedgerID: l.ID, Kind: "buy", InstrumentKey: fmt.Sprintf("S%d", i%500), Currency: "RUB", Amount: "10", Quantity: "1", OccurredAt: at, Sequence: int64(i + 1), Status: "posted", Version: 1}
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
	for i := 0; i < 500; i++ {
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
		if point.State != "ready" || point.Positions != 500 || point.PricedPositions != 500 || point.KnownValue.Cmp(want) != 0 {
			t.Fatal(i, point, want)
		}
	}
	t.Logf("100000 operations, 500 instruments, ten-year request, 31 valued days: %s", elapsed)
	for _, last := range []string{from.AddDate(0, 0, 30).Format(time.DateOnly), "2024-01-08"} {
		began = time.Now()
		h, err := repo.TrackerHistory(ctx, tenant, actor, nav.TrackerHistoryRequest{LedgerIDs: ids, Currency: "RUB", FXPolicyKey: policy.Key, From: from.Format(time.DateOnly), To: last, ContributionOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(h.Points) != 1 || h.Baseline == nil || len(h.Contributions) != 5000 {
			t.Fatal("attribution scale shape", len(h.Points), len(h.Contributions))
		}
		for _, r := range h.Contributions {
			if last == "2024-01-08" {
				if r.Profit != nil {
					t.Fatal("stale decade-old quote became an end valuation")
				}
			} else if r.Profit == nil || r.Profit.Sign() != 0 {
				t.Fatal("flat-price exact attribution", r)
			}
		}
		t.Logf("100000 operations, 500 instruments, contribution boundaries through %s: %s", last, time.Since(began))
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
	t.Logf("100000 operations, 500 instruments, one-security 31-day signed scope: %s", time.Since(began))
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
