package postgres

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"io"
	"strings"
	"testing"
	"time"
)

func TestInvestmentPositionsAndUnownedInstrumentPrices(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Positions','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('positions@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", at, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	version := int64(1)
	mutate := func(kind, key, quantity, amount, fee, tax string, when time.Time) {
		t.Helper()
		_, e := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: version, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: kind, InstrumentKey: key, Quantity: quantity, Amount: amount, Currency: "RUB", Fee: fee, Tax: tax, OccurredAt: when}})
		if e != nil {
			t.Fatal(e)
		}
		version++
	}
	mutate("deposit", "", "", "1000", "0", "0", at)
	mutate("buy", "S", "1", "100", "1", "0", at.Add(time.Hour))
	mutate("dividend", "S", "", "10", "0", "1", at.Add(2*time.Hour))
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e = repo.ConfigureSource(ctx, scope, "Test", true); e != nil {
		t.Fatal(e)
	}
	profile := nav.ConventionProfile{Key: "positions", Title: "Reviewed prices", Evidence: "Synthetic", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{}}
	for _, key := range []string{"S", "U"} {
		instrument := md.Instrument{ID: uuid.NewString(), Ticker: key, Name: "Synthetic " + key, ISIN: "TEST" + key, ClassCode: "TEST", Kind: "share", Currency: "RUB"}
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
		if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: key, RequestKey: uuid.NewString(), Reason: "Verified synthetic listing", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: at, Evidence: "Synthetic"}}}); e != nil {
			t.Fatal(e)
		}
		prices := []string{"120", "130"}
		if key == "U" {
			prices = []string{"35", "40"}
		}
		for i, price := range prices {
			if _, e = repo.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: instrument.ID, Kind: "last_price", At: at.AddDate(0, 0, i).Add(4 * time.Hour), SourceKind: "exchange", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Currency: "RUB", Price: price, Complete: true}}); e != nil {
				t.Fatal(e)
			}
		}
		profile.Periods[key] = []nav.PriceConventionPeriod{{Scope: scope, InstrumentID: instrument.ID, From: at, Policy: nav.SeriesPolicy{Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", MaxQuoteAge: 24 * time.Hour}}}
	}
	if _, e = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0); e != nil {
		t.Fatal(e)
	}
	policy := md.FXPolicy{Key: "market", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	for i, rate := range []string{"10", "20"} {
		day := at.AddDate(0, 0, i)
		if _, e = repo.SaveFXPoints(ctx, tenant, "market", 1, []md.FXPoint{{From: "USD", To: "RUB", Rate: rate, At: day, AvailableAt: day, ValidUntil: day.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: uuid.NewString()}}); e != nil {
			t.Fatal(e)
		}
	}
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "USD", FXPolicyKey: "market", ConventionKey: "positions"}
	if _, e = repo.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-02"); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if worked, e := repo.RunDailyNAVOnce(ctx, tenant); e != nil || !worked {
			t.Fatal(worked, e)
		}
	}
	key, _, e := navSeries(q)
	if e != nil {
		t.Fatal(e)
	}
	holdings, e := repo.InvestmentPositions(ctx, tenant, actor, key, "2024-01-01", nil)
	if e != nil || len(holdings.Rows) != 1 {
		t.Fatal(holdings, e)
	}
	row := holdings.Rows[0]
	if !row.HistoricalCost.CleanComplete || row.HistoricalCost.Clean.RatString() != "101/10" || !row.HistoricalCost.AccruedComplete || row.HistoricalCost.Accrued.Sign() != 0 {
		t.Fatal(row.HistoricalCost)
	}
	if row.Position.CleanCost != "101" || row.Value.RatString() != "12" || row.UnrealizedClean.RatString() != "19" || row.NativePrice.Clean.RatString() != "120" || row.Payments[0].Net.RatString() != "9" || holdings.Metadata["S"].ISIN != "TESTS" {
		t.Fatal(row, holdings.Metadata)
	}
	prices, e := repo.InvestmentInstrumentPrices(ctx, tenant, actor, key, "U", "2024-01-01", "2024-01-02", nil)
	if e != nil || len(prices.Days) != 2 || prices.Days[0].NativeClean.RatString() != "35" || prices.Days[0].TargetClean.RatString() != "7/2" || prices.Days[1].TargetClean.RatString() != "2" {
		t.Fatal(prices, e)
	}
	profit, e := repo.ProfitForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-02")
	if e != nil || profit.Performance.Profit == nil || profit.Performance.Profit.RatString() != "-509/10" || !profit.Attribution.Complete {
		t.Fatal("historical profit", profit, e)
	}
	if !profit.Instruments.Complete || len(profit.Instruments.Rows) != 1 || profit.Instruments.Rows[0].Known.RatString() != "-11/2" || profit.Instruments.Rows[0].Closed || profit.Instruments.Rows[0].Metadata.ISIN != "TESTS" || profit.Instruments.Remainder.Known.RatString() != "-227/5" {
		t.Fatal("instrument market FX attribution", profit.Instruments)
	}
	want := map[string]string{"unrealized_clean_change": "-9/20", "cash_fx": "-227/5", "fx_residual": "-101/20"}
	for _, c := range profit.Attribution.Components {
		if expected, exists := want[c.Kind]; exists && c.Known.RatString() != expected {
			t.Fatal(c, expected)
		}
	}
	overview, e := repo.OverviewForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-02")
	if e != nil || overview.NAV.State != "ready" || overview.NAV.Result.KnownValue.RatString() != "519/10" || overview.Composition.SecuritiesKnown.RatString() != "13/2" || overview.Composition.CashKnown.RatString() != "227/5" || overview.Performance.Profit.RatString() != "-509/10" || overview.Daily.Daily.RatString() != "-509/10" || overview.Performance.Generation != profit.Performance.Generation {
		t.Fatal(overview, e)
	}
	if _, e = repo.OverviewForSeries(ctx, uuid.NewString(), actor, key, "2024-01-02", "2024-01-02"); e == nil {
		t.Fatal("overview tenant access")
	}
	assertInvestmentNAVVariants(t, ctx, repo, q, key)
	assertInvestmentCurrentNAV(t, ctx, repo, q, key)
	neverHeld, e := repo.InvestmentInstrument(ctx, tenant, actor, key, "2024-01-01", "U", "", nil)
	if e != nil || len(neverHeld.Rows) != 0 || len(neverHeld.Operations) != 0 || neverHeld.Metadata["U"].Ticker != "U" {
		t.Fatal(neverHeld, e)
	}
	snapshotTx, e := pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = snapshotTx.Rollback(ctx) }()
	frozen, e := readInvestmentOverview(ctx, snapshotTx, tenant, actor, key, "2024-01-02", "2024-01-02", time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	frozenCSV, e := readInvestmentSeriesCSV(ctx, snapshotTx, tenant, actor, key, "2024-01-02", "2024-01-02")
	if e != nil {
		t.Fatal(e)
	}
	csvRows := decodeSeriesCSVTest(t, frozenCSV)
	if len(csvRows) != 1 || csvRows[0]["nav_numerator"] != "519" || csvRows[0]["nav_denominator"] != "10" || csvRows[0]["daily_profit_numerator"] != "-509" || csvRows[0]["daily_profit_denominator"] != "10" || csvRows[0]["scope"] != "full_registered_accounts" {
		t.Fatal(csvRows)
	}
	if _, e = repo.ExportSeries(ctx, uuid.NewString(), actor, key, "2024-01-02", "2024-01-02"); e == nil {
		t.Fatal("CSV tenant access")
	}
	mutate("sell", "S", "1", "150", "1", "0", at.AddDate(0, 0, 1).Add(5*time.Hour))
	holdings, e = repo.InvestmentPositions(ctx, tenant, actor, key, "2024-01-01", nil)
	if e != nil || holdings.Rows[0].Value.RatString() != "12" {
		t.Fatal("later operation invalidated unchanged past", holdings, e)
	}
	holdings, e = repo.InvestmentPositions(ctx, tenant, actor, key, "2024-01-02", nil)
	if e != nil || holdings.ValuationState != "stale" || holdings.Rows[0].Value != nil || holdings.Rows[0].Position.Quantity != "0" || holdings.Rows[0].Position.RealizedClean != "48" {
		t.Fatal("stale market or closed books lost", holdings, e)
	}
	oldOverview, e := readInvestmentOverview(ctx, snapshotTx, tenant, actor, key, "2024-01-02", "2024-01-02", time.Now().UTC())
	if e != nil || oldOverview.NAV.State != "ready" || oldOverview.Performance.Profit.Cmp(frozen.Performance.Profit) != 0 || oldOverview.Composition.CashKnown.Cmp(frozen.Composition.CashKnown) != 0 {
		t.Fatal("mixed overview snapshot", oldOverview, e)
	}
	oldCSV, e := readInvestmentSeriesCSV(ctx, snapshotTx, tenant, actor, key, "2024-01-02", "2024-01-02")
	if e != nil || !bytes.Equal(oldCSV.Data, frozenCSV.Data) {
		t.Fatal("mixed CSV snapshot", e)
	}
	if e = snapshotTx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	overview, e = repo.OverviewForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-02")
	if e != nil || overview.NAV.State != "stale" || overview.Composition != nil || overview.Performance.Profit != nil || overview.Daily.Daily != nil {
		t.Fatal("stale overview", overview, e)
	}
	staleCSV, e := repo.ExportSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-02")
	if e != nil {
		t.Fatal(e)
	}
	staleRows := decodeSeriesCSVTest(t, staleCSV)
	if staleRows[0]["nav_state"] != "stale" || staleRows[0]["nav_numerator"] != "" || staleRows[0]["daily_profit_numerator"] != "" || staleRows[0]["daily_profit_reason"] == "" {
		t.Fatal(staleRows)
	}
	for i := 0; i < 55; i++ {
		mutate("dividend", "S", "", "1", "0", "0", at.AddDate(0, 0, 1).Add(6*time.Hour+time.Duration(i)*time.Minute))
	}
	first, e := repo.InvestmentInstrument(ctx, tenant, actor, key, "2024-01-02", "S", "", nil)
	if e != nil || len(first.Operations) != 50 || first.NextCursor == "" {
		t.Fatal(first, e)
	}
	next, e := repo.InvestmentInstrument(ctx, tenant, actor, key, "2024-01-02", "S", first.NextCursor, nil)
	if e != nil || len(next.Operations) != 8 || next.NextCursor != "" {
		t.Fatal(next, e)
	}
	ids := map[string]bool{}
	for _, page := range []nav.InstrumentDetails{first, next} {
		for _, op := range page.Operations {
			if ids[op.ID] {
				t.Fatal("duplicate operation")
			}
			ids[op.ID] = true
		}
	}
	markers, e := repo.InvestmentInstrumentPrices(ctx, tenant, actor, key, "S", "2024-01-01", "2024-01-02", nil)
	if e != nil || len(markers.Markers) != 2 || markers.Markers[0].Kind != "buy" || markers.Markers[1].Kind != "sell" {
		t.Fatal("complete trade markers independent of operation pagination", markers, e)
	}
	mutate("dividend", "S", "", "1", "0", "0", at.AddDate(0, 0, 1).Add(8*time.Hour))
	if _, e = repo.InvestmentInstrument(ctx, tenant, actor, key, "2024-01-02", "S", first.NextCursor, nil); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("cursor did not reject concurrent journal change", e)
	}
	if _, e = repo.BuildDailyNAV(ctx, q, "2024-01-02"); e != nil {
		t.Fatal(e)
	}
	soldProfit, e := repo.ProfitForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-02")
	if e != nil || !soldProfit.Instruments.Complete || len(soldProfit.Instruments.Rows) != 1 || !soldProfit.Instruments.Rows[0].Closed || soldProfit.Instruments.Rows[0].Known.RatString() != "-7/4" || soldProfit.Instruments.Remainder.Known.RatString() != "-227/5" {
		t.Fatal("closed instrument and 56 dividends", soldProfit.Instruments, e)
	}
	groupID := uuid.NewString()
	if _, e = repo.SaveInstrumentGroups(ctx, tenant, actor, nav.SaveInstrumentGroups{InstrumentGroups: nav.InstrumentGroups{Groups: []nav.InstrumentGroup{{ID: groupID, Name: "Held"}}, Memberships: []nav.GroupMembership{{LedgerID: ledger.ID, InstrumentKey: "S", GroupID: groupID}}}, RequestKey: uuid.NewString(), Reason: "Test group selection"}); e != nil {
		t.Fatal(e)
	}
	filtered, e := repo.InvestmentInstrumentPrices(ctx, tenant, actor, key, "S", "2024-01-01", "2024-01-02", []string{""})
	if e != nil || len(filtered.Markers) != 0 || len(filtered.Days) != 2 || filtered.Days[0].NativeClean == nil {
		t.Fatal("groups must filter personal trades, not market prices", filtered, e)
	}
	card, e := repo.InvestmentInstrument(ctx, tenant, actor, key, "2024-01-02", "S", "", []string{""})
	if e != nil || len(card.Rows) != 0 || len(card.Operations) != 0 {
		t.Fatal("excluded group leaked holdings or operations", card, e)
	}
	if _, e = repo.InvestmentInstrument(ctx, tenant, actor, key, "2024-01-02", "S", "malformed", nil); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal(e)
	}
	if _, e = repo.InvestmentPositions(ctx, uuid.NewString(), actor, key, "2024-01-01", nil); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal(e)
	}
	if _, e = repo.InvestmentInstrumentPrices(ctx, tenant, actor, key, "U", "2023-12-31", "2024-01-02", nil); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal(e)
	}
	// A recurring FIFO cost must not become a fictitious FX/residual in RUB.
	fractional, e := svc.CreateLedger(ctx, tenant, actor, "Fractional FIFO", at, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	for i, op := range []d.InvestmentOperation{{Kind: "deposit", Amount: "100", OccurredAt: at}, {Kind: "buy", InstrumentKey: "S", Quantity: "3", Amount: "1", OccurredAt: at.Add(time.Hour)}, {Kind: "sell", InstrumentKey: "S", Quantity: "1", Amount: "1", OccurredAt: at.AddDate(0, 0, 1).Add(time.Hour)}} {
		op.LedgerID = fractional.ID
		op.Currency = "RUB"
		if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(i + 1), Operation: op}); e != nil {
			t.Fatal(e)
		}
	}
	fq := q
	fq.LedgerIDs = []string{fractional.ID}
	fq.Currency = "RUB"
	if _, e = repo.EnqueueDailyNAV(ctx, fq, "2024-01-01", "2024-01-02"); e != nil {
		t.Fatal(e)
	}
	for _, date := range []string{"2024-01-01", "2024-01-02"} {
		if _, e = repo.BuildDailyNAV(ctx, fq, date); e != nil {
			t.Fatal(e)
		}
	}
	fk, _, _ := navSeries(fq)
	fractionalPositions, e := repo.InvestmentPositions(ctx, tenant, actor, fk, "2024-01-02", nil)
	if e != nil || fractionalPositions.Rows[0].HistoricalCost.Clean.RatString() != "2/3" {
		t.Fatal(fractionalPositions, e)
	}
	usdq := fq
	usdq.Currency = "USD"
	if _, e = repo.EnqueueDailyNAV(ctx, usdq, "2024-01-02", "2024-01-02"); e != nil {
		t.Fatal(e)
	}
	usdk, _, _ := navSeries(usdq)
	converted, e := repo.InvestmentPositions(ctx, tenant, actor, usdk, "2024-01-02", nil)
	if e != nil || len(converted.Rows) != 1 || !converted.Rows[0].HistoricalCost.CleanComplete || converted.Rows[0].HistoricalCost.Clean.RatString() != "1/15" {
		t.Fatal(converted, e)
	}

	exact, e := repo.ProfitForSeries(ctx, tenant, actor, fk, "2024-01-02", "2024-01-02")
	if e != nil || !exact.Attribution.Complete || exact.Performance.Profit.RatString() != "-99" {
		t.Fatal(exact, e)
	}
	for _, c := range exact.Attribution.Components {
		if c.Kind == "fx_residual" && c.Known.Sign() != 0 {
			t.Fatal("rounded FIFO cost leaked into residual", c)
		}
		if c.Kind == "realized_clean" && c.Known.RatString() != "2/3" {
			t.Fatal(c)
		}
	}

	// Correction of the acquisition-day fixing changes cost even though this
	// registered watch starts later; no current-date rate or NAV is needed.
	if _, e = repo.SaveFXPoints(ctx, tenant, "market", 1, []md.FXPoint{{From: "USD", To: "RUB", Rate: "5", At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("b", 64), RecordReference: uuid.NewString()}}); e != nil {
		t.Fatal(e)
	}
	converted, e = repo.InvestmentPositions(ctx, tenant, actor, usdk, "2024-01-02", nil)
	if e != nil || converted.Rows[0].HistoricalCost.Clean.RatString() != "2/15" {
		t.Fatal(converted, e)
	}

	unknown, e := svc.CreateLedger(ctx, tenant, actor, "Partial overview", at, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	for i, op := range []d.InvestmentOperation{{Kind: "deposit", Amount: "100", OccurredAt: at}, {Kind: "buy", InstrumentKey: "UNMAPPED", Quantity: "1", Amount: "10", OccurredAt: at.Add(time.Hour)}} {
		op.LedgerID = unknown.ID
		op.Currency = "USD"
		if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(i + 1), Operation: op}); e != nil {
			t.Fatal(e)
		}
	}
	partialQ := q
	partialQ.LedgerIDs = []string{unknown.ID}
	if _, e = repo.EnqueueDailyNAV(ctx, partialQ, "2024-01-01", "2024-01-02"); e != nil {
		t.Fatal(e)
	}
	for _, date := range []string{"2024-01-01", "2024-01-02"} {
		if _, e = repo.BuildDailyNAV(ctx, partialQ, date); e != nil {
			t.Fatal(e)
		}
	}
	partialKey, _, _ := navSeries(partialQ)
	partialCSV, e := repo.ExportSeries(ctx, tenant, actor, partialKey, "2024-01-02", "2024-01-02")
	if e != nil {
		t.Fatal(e)
	}
	partialRows := decodeSeriesCSVTest(t, partialCSV)
	if partialRows[0]["cash_known_numerator"] != "90" || partialRows[0]["cash_complete"] != "true" || partialRows[0]["securities_complete"] != "false" || partialRows[0]["daily_profit_numerator"] != "" {
		t.Fatal(partialRows)
	}
	partialProfit, e := repo.ProfitForSeries(ctx, tenant, actor, partialKey, "2024-01-02", "2024-01-02")
	if e != nil || partialProfit.Instruments.Complete || partialProfit.Instruments.Rows[0].Complete || partialProfit.Instruments.Rows[0].Share != nil || partialProfit.Instruments.Remainder.Known != nil {
		t.Fatal("unknown instrument attribution", partialProfit, e)
	}
	partial, e := repo.OverviewForSeries(ctx, tenant, actor, partialKey, "2024-01-02", "2024-01-02")
	if e != nil || partial.NAV.State != "incomplete" || partial.NAV.Result.KnownValue.RatString() != "90" || !partial.Composition.CashComplete || partial.Composition.CashKnown.RatString() != "90" || partial.Composition.SecuritiesComplete || partial.Performance.Profit != nil || partial.Daily.Daily != nil {
		t.Fatal("partial must retain cash and refuse full profit", partial, e)
	}

}

func decodeSeriesCSVTest(t *testing.T, out nav.SeriesCSV) []map[string]string {
	t.Helper()
	zip, e := gzip.NewReader(bytes.NewReader(out.Data))
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = zip.Close() }()
	raw, e := io.ReadAll(zip)
	if e != nil {
		t.Fatal(e)
	}
	table, e := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if e != nil {
		t.Fatal(e)
	}
	rows := []map[string]string{}
	for _, values := range table[1:] {
		row := map[string]string{}
		for i, key := range table[0] {
			row[key] = values[i]
		}
		rows = append(rows, row)
	}
	return rows
}
