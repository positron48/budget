package postgres

import (
	"context"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"strings"
	"testing"
	"time"
)

func TestInvestmentDistributionMarketValueProfitAndRestore(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	journal := NewInvestmentRepo(pool)
	svc := inv.NewService(journal, NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Corporate event", at, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	var action d.InvestmentOperation
	for i, op := range []d.InvestmentOperation{
		{Kind: "deposit", Currency: "USD", Amount: "100", OccurredAt: at},
		{Kind: "buy", Currency: "USD", InstrumentKey: "parent", Quantity: "1", Amount: "100", OccurredAt: at.Add(time.Hour)},
		{Kind: "distribution", Currency: "USD", InstrumentKey: "parent", OccurredAt: at.AddDate(0, 0, 1).Add(time.Hour), Note: "Synthetic reviewed spin-off", Distribution: &d.InvestmentDistribution{EventKey: "spin-off", TargetInstrumentKey: "child", NewUnits: "1", OldUnits: "1", CostNumerator: "1", CostDenominator: "2", MarketValue: "40", MarketValueEvidence: "Reviewed total USD fair value at event"}},
	} {
		op.LedgerID = ledger.ID
		action, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", ExpectedLedgerVersion: int64(i + 1), RequestKey: uuid.NewString(), Operation: op})
		if e != nil {
			t.Fatal(e)
		}
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e = repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	profile := nav.ConventionProfile{Key: "fair", Title: "Synthetic", Evidence: "Synthetic", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{}}
	for _, key := range []string{"parent", "child"} {
		instrument := md.Instrument{ID: uuid.NewString(), Ticker: key, Name: key, Kind: "share", Currency: "USD", ClassCode: "TEST"}
		tx, e := pool.DB.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = saveMarketInstrument(ctx, tx, scope, instrument); e != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: key, RequestKey: key, Reason: "Synthetic", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: at, Evidence: "Synthetic"}}}); e != nil {
			t.Fatal(e)
		}
		profile.Periods[key] = []nav.PriceConventionPeriod{{Scope: scope, InstrumentID: instrument.ID, From: at, Policy: nav.SeriesPolicy{Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", MaxQuoteAge: 24 * time.Hour}}}
		for i := 0; i < 3; i++ {
			price := "70"
			if key == "parent" && i == 0 {
				price = "100"
			}
			if key == "child" {
				price = "45"
			}
			if _, e = repo.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: instrument.ID, Kind: "last_price", At: at.AddDate(0, 0, i).Add(4 * time.Hour), SourceKind: "exchange", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Currency: "USD", Price: price, Complete: true}}); e != nil {
				t.Fatal(e)
			}
		}
	}
	if _, e = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, md.FXPolicy{Key: "market", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}, 0); e != nil {
		t.Fatal(e)
	}
	saveFX := func(i int, rate string) {
		t.Helper()
		day := at.AddDate(0, 0, i)
		if _, e = repo.SaveFXPoints(ctx, tenant, "market", 1, []md.FXPoint{{From: "USD", To: "RUB", Rate: rate, At: day, AvailableAt: day, ValidUntil: day.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: uuid.NewString()}}); e != nil {
			t.Fatal(e)
		}
	}
	saveFX(0, "2")
	saveFX(2, "4")
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: "market", ConventionKey: "fair"}
	if _, e = repo.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-03"); e != nil {
		t.Fatal(e)
	}
	build := func() {
		t.Helper()
		for _, date := range []string{"2024-01-01", "2024-01-02", "2024-01-03"} {
			if _, e = repo.BuildDailyNAV(ctx, q, date); e != nil {
				t.Fatal(e)
			}
		}
	}
	build()
	key, _, _ := navSeries(q)
	read := func() nav.ProfitAnalytics {
		t.Helper()
		out, e := repo.ProfitForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-03")
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	incomplete := read()
	if incomplete.Instruments.Complete || incomplete.Instruments.Remainder.Known != nil {
		t.Fatal("missing event FX accepted", incomplete.Instruments)
	}
	for _, row := range incomplete.Instruments.Rows {
		if row.Components[3].Reason != "fx_missing" || row.Share != nil {
			t.Fatal(row)
		}
	}
	saveFX(1, "3")
	build()
	check := func(parent, child string) {
		t.Helper()
		out := read()
		if !out.Instruments.Complete || out.Performance.Profit == nil || out.Performance.Profit.RatString() != "260" || out.Instruments.Remainder.Known.Sign() != 0 {
			t.Fatal(out)
		}
		for _, row := range out.Instruments.Rows {
			want := parent
			if row.InstrumentKey == "child" {
				want = child
			}
			if row.Known.RatString() != want || row.Components[2].Known.Sign() != 0 {
				t.Fatal(row, want)
			}
		}
	}
	check("200", "60") // 40 USD at event FX 3; closing FX 4 is not reused.
	before, e := svc.Snapshot(ctx, tenant, actor, ledger.ID, at.AddDate(0, 0, 3))
	if e != nil {
		t.Fatal(e)
	}
	terms := *action.Distribution
	terms.MarketValue = "50"
	terms.MarketValueEvidence = "Corrected reviewed total"
	action.Distribution = &terms
	mutation := inv.Mutation{Action: "amend", ExpectedLedgerVersion: 4, ExpectedOperationVersion: action.Version, RequestKey: "correct", Reason: "Correct fair value only", Operation: action}
	action, e = svc.Mutate(ctx, tenant, actor, mutation)
	if e != nil {
		t.Fatal(e)
	}
	if retry, e := svc.Mutate(ctx, tenant, actor, mutation); e != nil || retry.ID != action.ID || retry.Version != action.Version {
		t.Fatal(retry, e)
	}
	build()
	check("230", "30")
	after, e := svc.Snapshot(ctx, tenant, actor, ledger.ID, at.AddDate(0, 0, 3))
	if e != nil || before.Cash[0] != after.Cash[0] || before.Positions[0].CleanCost != after.Positions[0].CleanCost {
		t.Fatal("allocation amended native facts", before, after, e)
	}
	bundle, e := journal.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal(e)
	}
	target, cleanup := withPg(t)
	defer cleanup()
	for _, entry := range []struct {
		sql  string
		args []any
	}{{`INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restore','RUB')`, []any{tenant}}, {`INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, []any{actor, actor + "@example.test"}}, {`INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, []any{tenant, actor}}} {
		if _, e = target.DB.Exec(context.Background(), entry.sql, entry.args...); e != nil {
			t.Fatal(e)
		}
	}
	restored := NewInvestmentRepo(target)
	if _, e = restored.RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); e != nil {
		t.Fatal(e)
	}
	rows, e := restored.ListOperations(ctx, tenant, ledger.ID, 20, 0)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, op := range rows {
		if op.ID == action.ID {
			found = true
			if op.Distribution == nil || op.Distribution.MarketValue != "50" || op.Distribution.MarketValueEvidence != "Corrected reviewed total" {
				t.Fatal(op)
			}
		}
	}
	if !found {
		t.Fatal("restored distribution missing")
	}
	restoredBundle, e := restored.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(restoredBundle.Data); e != nil {
		t.Fatal(e)
	}
}
