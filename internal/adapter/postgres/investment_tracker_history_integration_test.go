package postgres

import (
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"strings"
	"testing"
	"time"
)

func TestInvestmentTrackerHistoryUsesActualDailyPositionsAndMarketFX(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	l, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "History", StartedAt: day.AddDate(5, 0, 0), PortfolioTracking: true}, "tracker")
	if e != nil {
		t.Fatal(e)
	}
	for i, op := range []d.InvestmentOperation{
		{Kind: "buy", Quantity: "2", Amount: "1600", OccurredAt: day.Add(time.Hour)},
		{Kind: "split", SplitNew: "2", SplitOld: "1", OccurredAt: day.Add(25 * time.Hour)},
		{Kind: "sell", Quantity: "1", Amount: "700", OccurredAt: day.Add(49 * time.Hour)},
		{Kind: "dividend", Amount: "100", OccurredAt: day.Add(50 * time.Hour)},
		{Kind: "sell", Quantity: "3", Amount: "2100", OccurredAt: day.Add(145 * time.Hour)},
	} {
		op.LedgerID, op.Currency, op.InstrumentKey = l.ID, "RUB", "S"
		if op.Kind == "split" {
			op.Currency = ""
		}
		if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(i + 1), Operation: op}); e != nil {
			t.Fatal(e)
		}
	}
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e = repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "S", Name: "Synthetic", Kind: "share", Currency: "USD"}
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
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "map", Reason: "Synthetic", Periods: []md.MappingPeriod{{Provider: "tinvest", AccessScope: "personal", InstrumentID: instrument.ID, Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: day.Add(-48 * time.Hour), Evidence: "Synthetic"}}}); e != nil {
		t.Fatal(e)
	}
	for _, p := range []struct {
		offset   int
		price    string
		complete bool
	}{{-1, "10", true}, {0, "12", true}, {1, "7", true}, {2, "9", false}, {4, "8", true}} {
		o := md.Observation{InstrumentID: instrument.ID, Kind: "daily_candle", At: day.AddDate(0, 0, p.offset), SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "provider_unspecified", Price: p.price, Open: p.price, High: p.price, Low: p.price, Volume: "1", Complete: p.complete}
		if _, e = repo.SaveObservations(ctx, scope, []md.Observation{o}); e != nil {
			t.Fatal(e)
		}
	}
	policy := md.FXPolicy{Key: "history-market", Mode: "market", Source: "synthetic", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "One unit", ValidityConvention: "Explicit"}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	fx := []md.FXPoint{}
	for i, rate := range []string{"80", "90", "100", "110", "120", "", "140"} {
		if rate == "" {
			continue
		}
		at := day.AddDate(0, 0, i)
		fx = append(fx, md.FXPoint{From: "USD", To: "RUB", Rate: rate, At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: "synthetic/" + at.Format(time.DateOnly)})
	}
	if _, e = repo.SaveFXPoints(ctx, tenant, policy.Key, 1, fx); e != nil {
		t.Fatal(e)
	}
	q := nav.TrackerHistoryRequest{LedgerIDs: []string{l.ID}, Currency: "RUB", FXPolicyKey: policy.Key, From: "2020-01-01", To: "2020-01-07"}
	r, e := repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || len(r.Points) != 7 {
		t.Fatal(r, e)
	}
	for i, want := range []string{"1600", "2160", "2100"} {
		p := r.Points[i]
		if p.State != "ready" || p.KnownValue.RatString() != want || p.Positions != 1 || p.PricedPositions != 1 {
			t.Fatal("historical units or FX used current state", i, p)
		}
	}
	if r.Points[3].State != "incomplete" || r.Points[3].PricedPositions != 0 {
		t.Fatal("partial candle concealed by prior close", r.Points[3])
	}
	if r.Points[5].State != "incomplete" || r.Points[5].KnownValue.Sign() != 0 {
		t.Fatal("missing historical FX substituted", r.Points[5])
	}
	if r.Points[6].State != "ready" || r.Points[6].KnownValue.Sign() != 0 || r.Points[6].Positions != 0 {
		t.Fatal("closed security or payouts counted in value", r.Points[6])
	}
	performanceRequest := q
	performanceRequest.To = "2020-01-03"
	performanceRequest.PerformanceFrom = q.From
	performanceHistory, e := repo.TrackerHistory(ctx, tenant, actor, performanceRequest)
	if e != nil || performanceHistory.Baseline == nil || performanceHistory.Baseline.Date != "2019-12-31" || performanceHistory.Baseline.State != "ready" || performanceHistory.Baseline.KnownValue.Sign() != 0 {
		t.Fatal("performance baseline", performanceHistory, e)
	}
	for i, want := range []string{"1600", "0", "-800"} {
		if performanceHistory.Points[i].StrategyFlow == nil || performanceHistory.Points[i].StrategyFlow.RatString() != want {
			t.Fatal("securities flows need no funding and include sales/payouts", i, performanceHistory.Points[i])
		}
	}
	fullPageRequest := q
	fullPageRequest.To = "2020-02-10"
	fullPageRequest.PerformanceFrom = q.From
	fullPage, e := repo.TrackerHistory(ctx, tenant, actor, fullPageRequest)
	if e != nil || len(fullPage.Points) != 31 || fullPage.Baseline == nil || fullPage.NextDate != "2020-02-01" {
		t.Fatal("baseline must not consume a chart page slot", fullPage, e)
	}
	basis := r.BasisKey
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: instrument.ID, Kind: "last_price", At: day, SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "provider_unspecified", Price: "9999", Complete: true}}); e != nil {
		t.Fatal(e)
	}
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || r.BasisKey != basis || r.Points[0].KnownValue.RatString() != "1600" {
		t.Fatal("intraday price changed closed history", r, e)
	}
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: instrument.ID, Kind: "daily_candle", At: day.Add(-24 * time.Hour), SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "provider_unspecified", Price: "11", Open: "11", High: "11", Low: "11", Volume: "1", Complete: true}}); e != nil {
		t.Fatal(e)
	}
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || r.BasisKey == basis || r.Points[0].KnownValue.RatString() != "1760" {
		t.Fatal("corrected candle did not fence paging", r, e)
	}
	basis = r.BasisKey
	fx[0].Rate = "81"
	if _, e = repo.SaveFXPoints(ctx, tenant, policy.Key, 1, fx); e != nil {
		t.Fatal(e)
	}
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || r.BasisKey == basis || r.Points[0].KnownValue.RatString() != "1782" {
		t.Fatal("corrected FX did not fence paging", r, e)
	}
	basis = r.BasisKey
	// Provider workers may fetch unrelated papers or newer sessions while a
	// historical chart is paged. Neither changes the requested past horizon.
	for _, observation := range []md.Observation{
		{InstrumentID: uuid.NewString(), Kind: "daily_candle", At: day, SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "provider_unspecified", Price: "20", Open: "20", High: "20", Low: "20", Volume: "1", Complete: true},
		{InstrumentID: instrument.ID, Kind: "daily_candle", At: day.AddDate(0, 0, 20), SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "provider_unspecified", Price: "20", Open: "20", High: "20", Low: "20", Volume: "1", Complete: true},
	} {
		if _, e = repo.SaveObservations(ctx, scope, []md.Observation{observation}); e != nil {
			t.Fatal(e)
		}
	}
	futureFX := fx[0]
	futureFX.At, futureFX.AvailableAt, futureFX.ValidUntil = day.AddDate(0, 0, 20), day.AddDate(0, 0, 20), day.AddDate(0, 0, 21)
	if _, e = repo.SaveFXPoints(ctx, tenant, policy.Key, 1, []md.FXPoint{futureFX}); e != nil {
		t.Fatal(e)
	}
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || r.BasisKey != basis || r.Points[0].KnownValue.RatString() != "1782" {
		t.Fatal("unrelated or future evidence invalidated past history", r, e)
	}
	rows, e := pool.DB.Query(ctx, "EXPLAIN "+trackerHistoryBasisSQL, tenant, []string{"S"}, q.LedgerIDs, func() time.Time { at, _ := nav.DayCutoff(q.To); return at }())
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var plan string
		if e = rows.Scan(&plan); e != nil {
			t.Fatal(e)
		}
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	q.To = "2020-03-01"
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || len(r.Points) != 31 || r.NextDate != "2020-02-01" {
		t.Fatal("pagination", r.NextDate, e)
	}
	basis = r.BasisKey
	q.From = r.NextDate
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || r.NextDate != "" || len(r.Points) != 30 || r.BasisKey != basis {
		t.Fatal("second page", r, e)
	}
	_, other, outsider := acceptanceOwner(t, pool)
	if _, e = repo.RegisterFXPolicy(ctx, other, outsider, policy, 0); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.TrackerHistory(ctx, other, outsider, q); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("cross-tenant ledger leaked", e)
	}
	q.From, q.To = "2020-01-01", "2020-01-01"
	if e = repo.ConfigureSource(ctx, scope, "Synthetic", false); e != nil {
		t.Fatal(e)
	}
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || r.Points[0].State != "incomplete" || r.Points[0].KnownValue.Sign() != 0 {
		t.Fatal("disabled source used", r, e)
	}
	if e = repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: 6, Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "buy", InstrumentKey: "UNKNOWN-BASIS", Currency: "RUB", Quantity: "2", Amount: "10", OccurredAt: day.Add(2 * time.Hour)}}); e != nil {
		t.Fatal(e)
	}
	unknown := md.Instrument{ID: uuid.NewString(), Ticker: "U", Name: "Unconfirmed candle basis", Kind: "share", Currency: "USD"}
	tx, e = pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, scope, unknown); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "UNKNOWN-BASIS", RequestKey: "unknown-map", Reason: "Synthetic", Periods: []md.MappingPeriod{{Provider: "tinvest", AccessScope: "personal", InstrumentID: unknown.ID, Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: day.Add(-48 * time.Hour), Evidence: "Synthetic"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: unknown.ID, Kind: "daily_candle", At: day.Add(-24 * time.Hour), SourceKind: "exchange", Adjustment: "provider_unspecified", PriceUnit: "provider_unspecified", Price: "100", Open: "100", High: "100", Low: "100", Volume: "1", Complete: true}}); e != nil {
		t.Fatal(e)
	}
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || r.Points[0].State != "incomplete" || r.Points[0].PricedPositions != 1 || r.Points[0].KnownValue.RatString() != "1782" {
		t.Fatal("unknown candle basis valued as raw price", r, e)
	}
	found := false
	for _, issue := range r.Points[0].Issues {
		if issue.Code == "unadjusted_price_required" {
			found = true
		}
	}
	if !found {
		t.Fatal("uncertain adjustment reason missing", r.Points[0].Issues)
	}
	// A native historical source fixes only the historical basis. Live prices
	// and current mappings stay on the broker listing.
	native := md.Scope{TenantID: tenant, Provider: "twelvedata", AccessScope: "personal"}
	if e = repo.ConfigureSource(ctx, native, "Synthetic raw US history", true); e != nil {
		t.Fatal(e)
	}
	nativeInstrument := unknown
	nativeInstrument.Detailed = true
	nativeInstrument.ID = uuid.NewString()
	tx, e = pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, native, nativeInstrument); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveObservations(ctx, native, []md.Observation{{InstrumentID: nativeInstrument.ID, Kind: "daily_candle", At: day.Add(-24 * time.Hour), SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "currency_per_unit", Currency: "USD", Price: "20", Open: "20", High: "20", Low: "20", Volume: "1", Complete: true}}); e != nil {
		t.Fatal(e)
	}
	old, e := repo.GetMapping(ctx, tenant, "UNKNOWN-BASIS")
	if e != nil {
		t.Fatal(e)
	}
	history := []md.MappingPeriod{{Provider: native.Provider, AccessScope: native.AccessScope, InstrumentID: nativeInstrument.ID, Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: day.Add(-48 * time.Hour), Evidence: "Explicit raw daily history"}}
	change := md.MappingChange{InstrumentKey: old.InstrumentKey, ExpectedVersion: old.Version, RequestKey: "native-history", Reason: "Use raw historical prices", Periods: old.Periods, HistoricalPeriods: &history}
	updated, e := repo.SaveMapping(ctx, tenant, actor, change)
	if e != nil || updated.Periods[0].InstrumentID != unknown.ID || updated.HistoricalPeriods[0].InstrumentID != nativeInstrument.ID {
		t.Fatal(updated, e)
	}
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || r.Points[0].State != "ready" || r.Points[0].PricedPositions != 2 || r.Points[0].KnownValue.RatString() != "5022" {
		t.Fatal("native raw history not used", r, e)
	}
	// An existing API client omitting the new field must not erase history.
	updated, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: old.InstrumentKey, ExpectedVersion: updated.Version, RequestKey: "old-client", Reason: "Current listing edit", Periods: updated.Periods})
	if e != nil || len(updated.HistoricalPeriods) != 1 {
		t.Fatal("old client erased history", updated, e)
	}
	if e = repo.ConfigureSource(ctx, native, "Synthetic raw US history", false); e != nil {
		t.Fatal(e)
	}
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || r.Points[0].State != "incomplete" || r.Points[0].KnownValue.RatString() != "1782" {
		t.Fatal("disabled native source fell through to broker history", r, e)
	}
	if e = repo.ConfigureSource(ctx, native, "Synthetic raw US history", true); e != nil {
		t.Fatal(e)
	}
	clearHistory := []md.MappingPeriod{}
	clearChange := md.MappingChange{InstrumentKey: old.InstrumentKey, ExpectedVersion: updated.Version, RequestKey: "clear-history", Reason: "Clear history explicitly", Periods: updated.Periods, HistoricalPeriods: &clearHistory}
	cleared, e := repo.SaveMapping(ctx, tenant, actor, clearChange)
	if e != nil || len(cleared.HistoricalPeriods) != 0 {
		t.Fatal(cleared, e)
	}
	if _, e = repo.SaveMapping(ctx, tenant, actor, clearChange); e != nil {
		t.Fatal("retry failed", e)
	}
	clearChange.HistoricalPeriods = nil
	if _, e = repo.SaveMapping(ctx, tenant, actor, clearChange); !errors.Is(e, md.ErrConflict) {
		t.Fatal("clear and preserve share idempotency hash", e)
	}
	// MOEX's session nominal travels with the percentage observation. A later
	// amortization must not use today's or the mapping's initial face value.
	bondScope := md.Scope{TenantID: tenant, Provider: "moex", AccessScope: "personal"}
	if e = repo.ConfigureSource(ctx, bondScope, "Synthetic native bond history", true); e != nil {
		t.Fatal(e)
	}
	bondInstrument := md.Instrument{Detailed: true, ID: uuid.NewString(), Ticker: "B", Name: "Amortizing bond", Kind: "bond", Currency: "RUB"}
	tx, e = pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, bondScope, bondInstrument); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	bondPeriods := []md.MappingPeriod{{Provider: "moex", AccessScope: "personal", InstrumentID: bondInstrument.ID, Kind: "bond", Currency: "RUB", Unit: "percent_of_nominal", Nominal: "1000", NominalCurrency: "RUB", From: day.Add(-48 * time.Hour), Evidence: "Session-specific native nominal"}}
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "B", RequestKey: "bond-history", Reason: "Reviewed native bond history", HistoricalPeriods: &bondPeriods}); e != nil {
		t.Fatal(e)
	}
	for i, nominal := range []string{"1000", "800", ""} {
		o := md.Observation{InstrumentID: bondInstrument.ID, Kind: "daily_candle", At: day.AddDate(0, 0, i-1), SourceKind: "exchange", Currency: "RUB", PriceUnit: "percent_of_nominal", Adjustment: "unadjusted", Price: "95", Open: "95", High: "95", Low: "95", Volume: "1", Complete: true, Nominal: nominal}
		if nominal != "" {
			o.NominalCurrency = "RUB"
		}
		if _, e = repo.SaveObservations(ctx, bondScope, []md.Observation{o}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: 7, Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "buy", InstrumentKey: "B", Currency: "RUB", Quantity: "2", Amount: "2000", OccurredAt: day.Add(3 * time.Hour)}}); e != nil {
		t.Fatal(e)
	}
	q.From, q.To = "2020-01-01", "2020-01-03"
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || len(r.Points) != 3 || r.Points[0].KnownValue.RatString() != "3682" || r.Points[1].KnownValue.RatString() != "3680" || r.Points[2].KnownValue.RatString() != "2100" || r.Points[2].PricedPositions != 1 {
		t.Fatal("session nominal not used or missing nominal guessed", r, e)
	}
}
