package postgres

import (
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestInvestmentTrackerGroupsReclassifyHistoryWithoutChangingJournal(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledgers := []string{}
	for i := 0; i < 2; i++ {
		l, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Synthetic", PortfolioTracking: true, StartedAt: time.Now()}, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		ledgers = append(ledgers, l.ID)
		qty, amount := "1", "100"
		if i == 1 {
			qty, amount = "2", "200"
		}
		if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", ExpectedLedgerVersion: 1, RequestKey: uuid.NewString(), Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "buy", InstrumentKey: "S", Currency: "RUB", Quantity: qty, Amount: amount, OccurredAt: at}}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", ExpectedLedgerVersion: 2, RequestKey: uuid.NewString(), Operation: d.InvestmentOperation{LedgerID: ledgers[1], Kind: "sell", InstrumentKey: "S", Currency: "RUB", Quantity: "2", Amount: "220", OccurredAt: at.AddDate(0, 0, 2)}}); e != nil {
		t.Fatal(e)
	}
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "S", Name: "Synthetic", Kind: "share", Currency: "RUB"}
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
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "mapping", Reason: "Synthetic", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: at, Evidence: "Synthetic"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: instrument.ID, Kind: "daily_candle", At: at, SourceKind: "exchange", Currency: "RUB", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Price: "100", Open: "100", Low: "100", High: "100", Volume: "1", Complete: true}}); e != nil {
		t.Fatal(e)
	}
	policy := md.FXPolicy{Key: "group-market", Mode: "market", Source: "synthetic", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "One unit", ValidityConvention: "Explicit"}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	q := nav.TrackerHistoryRequest{LedgerIDs: ledgers, Currency: "RUB", FXPolicyKey: policy.Key, From: "2024-01-02", To: "2024-01-02"}
	all, e := repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil || all.Points[0].KnownValue.RatString() != "300" {
		t.Fatal(all, e)
	}
	hash := func() string {
		var out string
		if e := pool.DB.QueryRow(ctx, `SELECT md5(jsonb_agg(to_jsonb(o) ORDER BY id)::text) FROM investment_operations o WHERE tenant_id=$1`, tenant).Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	before := hash()
	a, b := uuid.NewString(), uuid.NewString()
	p := nav.SaveInstrumentGroups{InstrumentGroups: nav.InstrumentGroups{Groups: []nav.InstrumentGroup{{ID: a, Name: "A"}, {ID: b, Name: "B"}}, Memberships: []nav.GroupMembership{{LedgerID: ledgers[0], InstrumentKey: "S", GroupID: a}, {LedgerID: ledgers[1], InstrumentKey: "S", GroupID: b}}}, RequestKey: "initial", Reason: "Synthetic classification"}
	saved, e := repo.SaveInstrumentGroups(ctx, tenant, actor, p)
	if e != nil {
		t.Fatal(e)
	}
	read := func(ids []string, want string) nav.TrackerHistory {
		q.GroupIDs = ids
		r, e := repo.TrackerHistory(ctx, tenant, actor, q)
		if e != nil || len(r.Points) != 1 || r.Points[0].KnownValue.RatString() != want || r.Points[0].State != "ready" {
			t.Fatal(ids, r, e)
		}
		return r
	}
	first := read([]string{a}, "100")
	read([]string{b}, "200")
	read([]string{a, b, a}, "300")
	read([]string{""}, "0")
	p.InstrumentGroups = saved
	p.Groups[0].Name = "Renamed"
	p.RequestKey = "rename"
	saved, e = repo.SaveInstrumentGroups(ctx, tenant, actor, p)
	if e != nil {
		t.Fatal(e)
	}
	renamed := read([]string{a}, "100")
	if renamed.BasisKey == first.BasisKey || renamed.ClassificationVersion != 2 {
		t.Fatal("classification failed to fence history")
	}
	p.InstrumentGroups = saved
	p.Memberships[0].GroupID = b
	p.Memberships[1].GroupID = b
	p.RequestKey = "move"
	saved, e = repo.SaveInstrumentGroups(ctx, tenant, actor, p)
	if e != nil {
		t.Fatal(e)
	}
	read([]string{a}, "0")
	read([]string{b}, "300")
	p.InstrumentGroups = saved
	p.Groups = []nav.InstrumentGroup{{ID: a, Name: "A"}}
	p.Memberships = nil
	p.RequestKey = "delete"
	if _, e = repo.SaveInstrumentGroups(ctx, tenant, actor, p); e != nil {
		t.Fatal(e)
	}
	read([]string{""}, "300")
	q.GroupIDs = []string{b}
	if _, e = repo.TrackerHistory(ctx, tenant, actor, q); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("deleted filter broadened scope", e)
	}
	full := read(nil, "300")
	if full.BasisKey != all.BasisKey || hash() != before {
		t.Fatal("groups changed full history or journal")
	}
	_, positions, e := repo.GetInstrumentGroups(ctx, tenant, actor)
	if e != nil || len(positions) != 2 {
		t.Fatal("closed account position disappeared", positions, e)
	}
	if _, e = nav.NormalizeTrackerHistory(nav.TrackerHistoryRequest{LedgerIDs: ledgers, GroupIDs: []string{a}, Currency: "RUB", FXPolicyKey: policy.Key, From: q.From, To: q.To, PerformanceFrom: q.From}, time.Now()); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal("group inherited account performance", e)
	}
}
