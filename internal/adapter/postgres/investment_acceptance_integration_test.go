package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"strings"
	"testing"
	"time"
)

func acceptanceOwner(t *testing.T, pool *Pool) (context.Context, string, string) {
	t.Helper()
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	if _, e := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Acceptance','RUB')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	return ctx, tenant, actor
}
func TestInvestmentAcceptanceC13DistinctBrokerExecutions(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", start, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "100", OccurredAt: start}}); e != nil {
		t.Fatal(e)
	}
	var doc map[string]any
	if e = json.Unmarshal(brokerLinkedTradeFixture(t), &doc); e != nil {
		t.Fatal(e)
	}
	ops := doc["operations"].([]any)
	for _, raw := range append([]any{}, ops...) {
		v := raw.(map[string]any)
		copy := map[string]any{}
		for k, value := range v {
			copy[k] = value
		}
		copy["id"] = v["id"].(string) + "-2"
		if _, ok := copy["parentOperationId"]; ok {
			copy["parentOperationId"] = "trade-2"
		}
		ops = append(ops, copy)
	}
	doc["operations"] = ops
	raw, _ := json.Marshal(ops)
	sum := sha256.Sum256(raw)
	doc["history_evidence"] = map[string]any{"version": 1, "complete": true, "pages": []map[string]any{{"count": 4, "items_sha256": hex.EncodeToString(sum[:]), "has_next": false, "cursor": "", "next_cursor": ""}}}
	raw, _ = json.Marshal(doc)
	repo := NewInvestmentImportRepo(pool)
	source, e := repo.SaveBrokerSnapshot(ctx, tenant, actor, raw)
	if e != nil {
		t.Fatal(e)
	}
	importer := imp.NewImportService(repo, NewTenantRepo(pool))
	ids := []string{}
	for i, id := range []string{"trade", "trade-2"} {
		p, e := importer.PreviewBrokerTrade(ctx, tenant, actor, source, id, ledger.ID)
		if e != nil || !p.PostingAllowed {
			t.Fatalf("C13 preview %s: %+v %v", id, p, e)
		}
		post := imp.BrokerTradePost{SnapshotID: source, BrokerOperationID: id, LedgerID: ledger.ID, RequestKey: id, ExpectedLedgerVersion: int64(i + 2), ReconciliationConfirmed: true, Reason: "Confirmed distinct executions"}
		v, e := repo.PostBrokerTrade(ctx, tenant, actor, post)
		if e != nil {
			t.Fatal("C13 second identical trade must survive", e)
		}
		ids = append(ids, v.OperationID)
		countFacts := func() [3]int64 {
			t.Helper()
			var counts [3]int64
			if e := pool.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM investment_operations WHERE tenant_id=$1),(SELECT count(*) FROM investment_cash_legs WHERE tenant_id=$1),(SELECT count(*) FROM investment_security_legs WHERE tenant_id=$1)`, tenant).Scan(&counts[0], &counts[1], &counts[2]); e != nil {
				t.Fatal(e)
			}
			return counts
		}
		beforeRetry := countFacts()
		if retry, e := repo.PostBrokerTrade(ctx, tenant, actor, post); e != nil || retry != v {
			t.Fatal("C12 retry", retry, e)
		}
		if countFacts() != beforeRetry {
			t.Fatal("C12 retry changed operation or leg counts")
		}
		post.RequestKey += "-again"
		post.ExpectedLedgerVersion++
		if _, e = repo.PostBrokerTrade(ctx, tenant, actor, post); !errors.Is(e, imp.ErrConflict) {
			t.Fatal("same identity reposted", e)
		}
	}
	if ids[0] == ids[1] {
		t.Fatal("distinct identities collapsed")
	}
	snapshot, e := svc.Snapshot(ctx, tenant, actor, ledger.ID, start.AddDate(0, 2, 0))
	if e != nil || len(snapshot.Positions) != 1 || snapshot.Positions[0].Quantity != "4" || snapshot.Positions[0].CleanCost != "42" || snapshot.Cash[0].Economic != "58" {
		t.Fatal(snapshot, e)
	}
	// A manual counterpart has no immutable provider identity; it must still
	// block posting rather than being silently treated as a distinct execution.
	var single map[string]any
	if e = json.Unmarshal(raw, &single); e != nil {
		t.Fatal(e)
	}
	third := single["operations"].([]any)[:2]
	for _, v := range third {
		row := v.(map[string]any)
		row["id"] = row["id"].(string) + "-3"
		if _, ok := row["parentOperationId"]; ok {
			row["parentOperationId"] = "trade-3"
		}
	}
	single["operations"] = third
	items, _ := json.Marshal(third)
	sum3 := sha256.Sum256(items)
	single["history_evidence"] = map[string]any{"version": 1, "complete": true, "pages": []map[string]any{{"count": 2, "items_sha256": hex.EncodeToString(sum3[:]), "has_next": false, "cursor": "", "next_cursor": ""}}}
	thirdRaw, _ := json.Marshal(single)
	thirdSource, e := repo.SaveBrokerSnapshot(ctx, tenant, actor, thirdRaw)
	if e != nil {
		t.Fatal(e)
	}
	before, e := importer.PreviewBrokerTrade(ctx, tenant, actor, thirdSource, "trade-3", ledger.ID)
	if e != nil || !before.PostingAllowed {
		t.Fatal(before, e)
	}
	manual := before.Operation
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "manual", ExpectedLedgerVersion: 4, Operation: manual}); e != nil {
		t.Fatal(e)
	}
	blocked, e := importer.PreviewBrokerTrade(ctx, tenant, actor, thirdSource, "trade-3", ledger.ID)
	if e != nil || blocked.PostingAllowed || len(blocked.JournalMatches) != 1 {
		t.Fatal("unlinked counterpart bypassed", blocked, e)
	}
	if _, e = repo.PostBrokerTrade(ctx, tenant, actor, imp.BrokerTradePost{SnapshotID: thirdSource, BrokerOperationID: "trade-3", LedgerID: ledger.ID, RequestKey: "third", ExpectedLedgerVersion: 5, ReconciliationConfirmed: true, Reason: "Cannot bypass unlinked match"}); !errors.Is(e, imp.ErrConflict) {
		t.Fatal(e)
	}
	rows, e := pool.DB.Query(ctx, "EXPLAIN "+distinctBrokerCandidatesSQL, tenant, start.AddDate(0, 1, 0).Add(-24*time.Hour), start.AddDate(0, 1, 0).Add(24*time.Hour), []string{"tinvest:account:trade-2"})
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var plan string
		if e = rows.Scan(&plan); e != nil {
			t.Fatal(e)
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
}

func TestInvestmentAcceptanceC16HistoricalProfitAndC17CorrectedClose(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", at, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	for i, op := range []d.InvestmentOperation{{Kind: "deposit", Amount: "100", OccurredAt: at}, {Kind: "buy", InstrumentKey: "S", Quantity: "1", Amount: "100", OccurredAt: at.AddDate(0, 0, 1).Add(time.Hour)}, {Kind: "sell", InstrumentKey: "S", Quantity: "1", Amount: "110", OccurredAt: at.AddDate(0, 0, 3).Add(time.Hour)}} {
		op.LedgerID = ledger.ID
		op.Currency = "USD"
		if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(i + 1), Operation: op}); e != nil {
			t.Fatal(e)
		}
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e = repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "S", Name: "Synthetic", ISIN: "TESTS", ClassCode: "TEST", Kind: "share", Currency: "USD"}
	tx, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, scope, instrument); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "mapping", Reason: "Synthetic evidence", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: at, Evidence: "Synthetic"}}}); e != nil {
		t.Fatal(e)
	}
	profile := nav.ConventionProfile{Key: "acceptance", Title: "Synthetic prices", Evidence: "Synthetic", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{"S": {{Scope: scope, InstrumentID: instrument.ID, From: at, Policy: nav.SeriesPolicy{Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", MaxQuoteAge: 24 * time.Hour}}}}}
	if _, e = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0); e != nil {
		t.Fatal(e)
	}
	policy := md.FXPolicy{Key: "market", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	var corrected md.Observation
	for i, price := range []string{"100", "100", "105", "110"} {
		day := at.AddDate(0, 0, i)
		quote := md.Observation{InstrumentID: instrument.ID, Kind: "last_price", At: day.Add(4 * time.Hour), SourceKind: "exchange", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Currency: "USD", Price: price, Complete: true}
		if _, e = repo.SaveObservations(ctx, scope, []md.Observation{quote}); e != nil {
			t.Fatal(e)
		}
		if i == 2 {
			corrected = quote
		}
		rate := "80"
		if i >= 2 {
			rate = "90"
		}
		if _, e = repo.SaveFXPoints(ctx, tenant, "market", 1, []md.FXPoint{{From: "USD", To: "RUB", Rate: rate, At: day, AvailableAt: day, ValidUntil: day.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: uuid.NewString()}}); e != nil {
			t.Fatal(e)
		}
	}
	oldGeneration := map[string]int64{}
	queries := map[string]nav.Request{}
	keys := map[string]string{}
	for _, ccy := range []string{"USD", "RUB"} {
		q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: ccy, FXPolicyKey: "market", ConventionKey: "acceptance"}
		queries[ccy] = q
		if _, e = repo.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-04"); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 4; i++ {
			if _, e = repo.BuildDailyNAV(ctx, q, at.AddDate(0, 0, i).Format(time.DateOnly)); e != nil {
				t.Fatal(e)
			}
		}
		history, e := repo.DailyNAVHistory(ctx, q, "2024-01-03", "2024-01-03")
		if e != nil || len(history) != 1 {
			t.Fatal(e)
		}
		oldGeneration[ccy] = history[0].Generation
		keys[ccy], _, _ = navSeries(q)
		v, e := repo.ProfitForSeries(ctx, tenant, actor, keys[ccy], "2024-01-02", "2024-01-04")
		want := "10"
		if ccy == "RUB" {
			want = "1900"
		}
		if e != nil || v.Performance.Profit == nil || v.Performance.Profit.RatString() != want {
			t.Fatalf("C16 %s: %+v %v", ccy, v, e)
		}
	}
	// Correct a historical quote without altering journal facts. Cached dependent
	// dates must become unavailable until rebuilt, across both display currencies.
	corrected.Price = "106"
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{corrected}); e != nil {
		t.Fatal(e)
	}
	for ccy, q := range queries {
		hist, e := repo.DailyNAVHistory(ctx, q, "2024-01-01", "2024-01-04")
		if e != nil || hist[0].State != "ready" || hist[1].State != "ready" || hist[2].State != "stale" || hist[2].Result != nil {
			t.Fatalf("C17 %s: %+v %v", ccy, hist, e)
		}
		for _, date := range []string{"2024-01-03", "2024-01-04"} {
			if _, e = repo.BuildDailyNAV(ctx, q, date); e != nil {
				t.Fatal(e)
			}
		}
		rebuilt, e := repo.DailyNAVHistory(ctx, q, "2024-01-03", "2024-01-03")
		if e != nil || len(rebuilt) != 1 || rebuilt[0].State != "ready" || rebuilt[0].Generation <= oldGeneration[ccy] {
			t.Fatal("C17 revision", rebuilt, e)
		}
		holdings, e := repo.InvestmentPositions(ctx, tenant, actor, keys[ccy], "2024-01-03", nil)
		if e != nil || len(holdings.Rows) != 1 {
			t.Fatal(holdings, e)
		}
		expected := "106"
		cost := "100"
		if ccy == "RUB" {
			expected = "9540"
			cost = "8000"
		}
		if holdings.Rows[0].Value.RatString() != expected || holdings.Rows[0].HistoricalCost.Clean.RatString() != cost {
			t.Fatal("C16 historical cost / C17 coherent positions", holdings.Rows[0])
		}
		v, e := repo.ProfitForSeries(ctx, tenant, actor, keys[ccy], "2024-01-02", "2024-01-03")
		want := "6"
		if ccy == "RUB" {
			want = "1540"
		}
		if e != nil || v.Performance.Profit == nil || v.Performance.Profit.RatString() != want {
			t.Fatalf("C17 recomputed %s: %+v %v", ccy, v, e)
		}
	}
}
