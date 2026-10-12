package postgres

import (
	"context"
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

func TestInvestmentLotsPaginationHistoryFXAndAccess(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Lots','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('lots@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	repo := NewMarketDataRepo(pool)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", at, "lots")
	if e != nil {
		t.Fatal(e)
	}
	version := int64(1)
	mutate := func(kind, quantity, amount string, when time.Time) {
		t.Helper()
		instrument := "S"
		if kind == "deposit" {
			instrument = ""
		}
		_, e := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: version, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: kind, InstrumentKey: instrument, Currency: "RUB", Quantity: quantity, Amount: amount, OccurredAt: when}})
		if e != nil {
			t.Fatal(e)
		}
		version++
	}
	mutate("deposit", "", "1000", at)
	for i := 0; i < 51; i++ {
		mutate("buy", "3", "1", at.Add(time.Minute))
	}
	mutate("sell", "1", "1", at.Add(2*time.Hour))
	policy := md.FXPolicy{Key: "market", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	saveRate := func(rate string) {
		t.Helper()
		if _, e = repo.SaveFXPoints(ctx, tenant, "market", 1, []md.FXPoint{{From: "USD", To: "RUB", Rate: rate, At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: uuid.NewString()}}); e != nil {
			t.Fatal(e)
		}
	}
	saveRate("10")
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "USD", FXPolicyKey: "market"}
	if _, e = repo.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-02"); e != nil {
		t.Fatal(e)
	}
	key, _, _ := navSeries(q)
	page, e := repo.InvestmentLots(ctx, tenant, actor, key, "2024-01-01", "S", "", nil)
	if e != nil || len(page.Rows) != 50 || page.Total != 51 || page.NextCursor == "" {
		t.Fatal(page, e)
	}
	first := page.Rows[0]
	if first.Lot.CleanCost.RatString() != "2/3" || first.HistoricalCost.Clean.RatString() != "1/15" || len(first.Changes) != 2 || first.Changes[1].Kind != "sell" || first.Changes[0].After.CleanCost.RatString() != "1" || first.FX.Rate.RatString() != "1/10" || !first.FX.Legs[0].Inverse || first.Origin.Kind != "buy" {
		t.Fatal(first)
	}
	next, e := repo.InvestmentLots(ctx, tenant, actor, key, "2024-01-01", "S", page.NextCursor, nil)
	if e != nil || len(next.Rows) != 1 || next.NextCursor != "" || next.Rows[0].Lot.OriginID == first.Lot.OriginID {
		t.Fatal(next, e)
	}
	empty, e := repo.InvestmentLots(ctx, tenant, actor, key, "2024-01-01", "UNOWNED", "", nil)
	if e != nil || empty.Total != 0 {
		t.Fatal(empty, e)
	}
	for _, wrong := range []struct{ tenant, actor, date, instrument, cursor string }{{uuid.NewString(), actor, "2024-01-01", "S", ""}, {tenant, uuid.NewString(), "2024-01-01", "S", ""}, {tenant, actor, "2024-01-03", "S", ""}, {tenant, actor, "2024-01-01", "OTHER", page.NextCursor}, {tenant, actor, "2024-01-01", "S", "invalid"}} {
		if _, e = repo.InvestmentLots(ctx, wrong.tenant, wrong.actor, key, wrong.date, wrong.instrument, wrong.cursor, nil); e == nil {
			t.Fatal("invalid read accepted", wrong)
		}
	}
	// Future operations are validated, but do not change the historical lots.
	mutate("sell", "2", "1", at.Add(25*time.Hour))
	older, e := repo.InvestmentLots(ctx, tenant, actor, key, "2024-01-01", "S", "", nil)
	if e != nil || older.Rows[0].Lot.Quantity.RatString() != "2" {
		t.Fatal(older, e)
	}
	if _, e = repo.InvestmentLots(ctx, tenant, actor, key, "2024-01-01", "S", page.NextCursor, nil); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal(e)
	}
	// Changing acquisition-day FX also fences pagination, independently of ledgers.
	saveRate("5")
	if _, e = repo.InvestmentLots(ctx, tenant, actor, key, "2024-01-01", "S", older.NextCursor, nil); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal(e)
	}
	corrected, e := repo.InvestmentLots(ctx, tenant, actor, key, "2024-01-01", "S", "", nil)
	if e != nil || corrected.Rows[0].HistoricalCost.Clean.RatString() != "2/15" {
		t.Fatal(corrected, e)
	}

	// Unknown opening cost, actual zero and a missing date are independent.
	opening, e := svc.CreateLedger(ctx, tenant, actor, "Opening", at, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	for i, op := range []d.InvestmentOperation{
		{Kind: "opening_position", InstrumentKey: "UNKNOWN", Quantity: "1", Note: "Missing acquisition report"},
		{Kind: "opening_position", InstrumentKey: "ZERO", Quantity: "1", OpeningCostKnown: true, OpeningAccruedKnown: true, Note: "Confirmed zero cost"},
		{Kind: "opening_position", InstrumentKey: "DATE", Quantity: "1", Amount: "2", OpeningCostKnown: true, OpeningAccruedKnown: true, Note: "Cost confirmed; acquisition date missing"},
	} {
		op.LedgerID = opening.ID
		op.Currency = "RUB"
		op.OccurredAt = at
		if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(i + 1), Operation: op}); e != nil {
			t.Fatal(e)
		}
	}
	oq := q
	oq.LedgerIDs = []string{opening.ID}
	if _, e = repo.EnqueueDailyNAV(ctx, oq, "2024-01-01", "2024-01-02"); e != nil {
		t.Fatal(e)
	}
	ok, _, _ := navSeries(oq)
	unknown, e := repo.InvestmentLots(ctx, tenant, actor, ok, "2024-01-01", "UNKNOWN", "", nil)
	if e != nil || unknown.Rows[0].Lot.CleanCost != nil || unknown.Rows[0].HistoricalCost.Clean != nil || unknown.Rows[0].FX != nil || !unknown.Rows[0].Lot.AcquiredAt.IsZero() {
		t.Fatal(unknown, e)
	}
	zero, e := repo.InvestmentLots(ctx, tenant, actor, ok, "2024-01-01", "ZERO", "", nil)
	if e != nil || zero.Rows[0].HistoricalCost.Clean.Sign() != 0 || !zero.Rows[0].HistoricalCost.CleanComplete || zero.Rows[0].FX != nil {
		t.Fatal(zero, e)
	}
	missingDate, e := repo.InvestmentLots(ctx, tenant, actor, ok, "2024-01-01", "DATE", "", nil)
	if e != nil || missingDate.Rows[0].HistoricalCost.Clean != nil || missingDate.Rows[0].HistoricalCost.Reasons[0] != "acquisition_date_missing" {
		t.Fatal(missingDate, e)
	}

	group, other := uuid.NewString(), uuid.NewString()
	if _, e = repo.SaveInstrumentGroups(ctx, tenant, actor, nav.SaveInstrumentGroups{InstrumentGroups: nav.InstrumentGroups{Groups: []nav.InstrumentGroup{{ID: group, Name: "Selected"}, {ID: other, Name: "Other"}}, Memberships: []nav.GroupMembership{{LedgerID: ledger.ID, InstrumentKey: "S", GroupID: group}}}, RequestKey: uuid.NewString(), Reason: "Reviewed selection"}); e != nil {
		t.Fatal(e)
	}
	grouped, e := repo.InvestmentLots(ctx, tenant, actor, key, "2024-01-01", "S", "", []string{group})
	if e != nil || grouped.Total != 51 {
		t.Fatal(grouped, e)
	}
	excluded, e := repo.InvestmentLots(ctx, tenant, actor, key, "2024-01-01", "S", "", []string{other})
	if e != nil || excluded.Total != 0 {
		t.Fatal(excluded, e)
	}
	if _, e = repo.InvestmentLots(ctx, tenant, actor, key, "2024-01-01", "S", corrected.NextCursor, nil); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal(e)
	}
	if _, e = repo.InvestmentLots(ctx, tenant, actor, key, "2024-01-01", "S", "", []string{uuid.NewString()}); e == nil {
		t.Fatal("unknown group accepted")
	}
}
