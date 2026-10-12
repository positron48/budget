package postgres

import (
	"context"
	"errors"
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

func TestInvestmentValuationConsistentSnapshot(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('NAV','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('nav@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", day, "create")
	if e != nil {
		t.Fatal(e)
	}
	deposit := inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "1000", OccurredAt: day.Add(time.Hour)}}
	if _, e = svc.Mutate(ctx, tenant, actor, deposit); e != nil {
		t.Fatal(e)
	}
	buy := inv.Mutation{Action: "create", RequestKey: "buy", ExpectedLedgerVersion: 2, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "S", Currency: "RUB", Amount: "100", Quantity: "1", OccurredAt: day.Add(2 * time.Hour)}}
	if _, e = svc.Mutate(ctx, tenant, actor, buy); e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e = repo.ConfigureSource(ctx, scope, "Test", true); e != nil {
		t.Fatal(e)
	}
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "S", Name: "Synthetic share", ClassCode: "TEST", Kind: "share", Currency: "RUB"}
	setup, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, setup, scope, instrument); e != nil {
		t.Fatal(e)
	}
	if e = setup.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "mapping", Reason: "Synthetic verified terms", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: day, Evidence: "Test evidence"}}}); e != nil {
		t.Fatal(e)
	}
	price := md.Observation{InstrumentID: instrument.ID, Kind: "last_price", At: day.Add(4 * time.Hour), SourceKind: "exchange", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Currency: "RUB", Price: "120", Complete: true}
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{price}); e != nil {
		t.Fatal(e)
	}
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, At: day.Add(6 * time.Hour), Currency: "RUB", FXPolicyKey: "market-test", Series: map[string]nav.SeriesPolicy{"S": {Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", MaxQuoteAge: 24 * time.Hour}}}
	check := func(got nav.Result, e error, want string, version int64) {
		t.Helper()
		if e != nil || !got.Complete || got.KnownValue.RatString() != want || got.LedgerVersions[ledger.ID] != version {
			t.Fatal(got, e)
		}
	}
	got, e := repo.ReadInvestmentValuation(ctx, q)
	check(got, e, "1020", 3)
	// Establish an MVCC snapshot, then commit a journal change AND price correction
	// on other connections before assembling NAV in the original transaction.
	frozen, e := pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = frozen.Rollback(ctx) }()
	var anchor int64
	if e = frozen.QueryRow(ctx, `SELECT version FROM investment_ledgers WHERE id=$1`, ledger.ID).Scan(&anchor); e != nil {
		t.Fatal(e)
	}
	deposit.RequestKey = "later"
	deposit.ExpectedLedgerVersion = 3
	deposit.Operation.Amount = "50"
	deposit.Operation.OccurredAt = day.Add(3 * time.Hour)
	if _, e = svc.Mutate(ctx, tenant, actor, deposit); e != nil {
		t.Fatal(e)
	}
	price.Price = "130"
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{price}); e != nil {
		t.Fatal(e)
	}
	got, e = readInvestmentValuation(ctx, frozen, q)
	check(got, e, "1020", 3)
	if e = frozen.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	got, e = repo.ReadInvestmentValuation(ctx, q)
	check(got, e, "1080", 4)
	// Missing conversion is a gap, then the approved persisted FX point feeds the
	// actual valuation including the exact inverse and its provenance.
	q.Currency = "USD"
	got, e = repo.ReadInvestmentValuation(ctx, q)
	if e != nil || got.Complete || got.KnownValue.Sign() != 0 {
		t.Fatal(got, e)
	}
	p := md.FXPolicy{Key: q.FXPolicyKey, Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic evidence", TimeConvention: "Explicit timestamps", ValueConvention: "Per one unit", ValidityConvention: "Explicit validity"}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, p, 0); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveFXPoints(ctx, tenant, p.Key, 1, []md.FXPoint{{From: "USD", To: "RUB", Rate: "10", At: day, AvailableAt: day, ValidUntil: day.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: "fixture/1"}}); e != nil {
		t.Fatal(e)
	}
	got, e = repo.ReadInvestmentValuation(ctx, q)
	check(got, e, "108", 4)
	if len(got.FXPaths["RUB"]) != 1 || !got.FXPaths["RUB"][0].Inverse {
		t.Fatal(got.FXPaths)
	}
	q.Series = nil
	got, e = repo.ReadInvestmentValuation(ctx, q)
	if e != nil || got.Complete || got.KnownValue.RatString() != "95" {
		t.Fatal("missing price policy hidden", got, e)
	}
	q.UserID = uuid.NewString()
	if _, e = repo.ReadInvestmentValuation(ctx, q); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("foreign reader", e)
	}
	q.UserID = actor
	q.LedgerIDs = append(q.LedgerIDs, q.LedgerIDs[0])
	if _, e = repo.ReadInvestmentValuation(ctx, q); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal("duplicate ledgers", e)
	}
}
