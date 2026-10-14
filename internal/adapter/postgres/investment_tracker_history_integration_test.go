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
	l, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "History", StartedAt: day.Add(-24 * time.Hour), PortfolioTracking: true}, "tracker")
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
	}{{-1, "10", true}, {0, "6", true}, {1, "7", true}, {2, "9", false}, {4, "8", true}} {
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
	rows, e := pool.DB.Query(ctx, "EXPLAIN "+trackerHistoryBasisSQL, tenant)
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
	q.From = r.NextDate
	r, e = repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || r.NextDate != "" || len(r.Points) != 30 {
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

}
