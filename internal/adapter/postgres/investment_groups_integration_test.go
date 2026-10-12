package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"testing"
	"time"
)

func TestInvestmentGroupsAtomicClassification(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	if _, e := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Groups','RUB');`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	journal := NewInvestmentRepo(pool)
	svc := inv.NewService(journal, NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	accounts := []string{}
	for i := 0; i < 2; i++ {
		l, e := svc.CreateLedger(ctx, tenant, actor, "Broker", at, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		accounts = append(accounts, l.ID)
		_, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "deposit", Currency: "RUB", Amount: "100", OccurredAt: at}})
		if e != nil {
			t.Fatal(e)
		}
		_, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: 2, Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "buy", InstrumentKey: "ABC", Currency: "RUB", Quantity: "1", Amount: "10", OccurredAt: at.Add(time.Hour)}})
		if e != nil {
			t.Fatal(e)
		}
	}
	repo := NewMarketDataRepo(pool)
	g, h := uuid.NewString(), uuid.NewString()
	p := nav.SaveInstrumentGroups{InstrumentGroups: nav.InstrumentGroups{Groups: []nav.InstrumentGroup{{ID: g, Name: "Strategy"}, {ID: h, Name: "Other"}}, Memberships: []nav.GroupMembership{{LedgerID: accounts[0], InstrumentKey: "ABC", GroupID: g}, {LedgerID: accounts[1], InstrumentKey: "ABC", GroupID: h}}}, RequestKey: "save", Reason: "Review"}
	type result struct {
		p nav.InstrumentGroups
		e error
	}
	ch := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() { v, e := repo.SaveInstrumentGroups(ctx, tenant, actor, p); ch <- result{v, e} }()
	}
	first, second := <-ch, <-ch
	if first.e != nil || second.e != nil || first.p.Version != 1 || second.p.Version != 1 {
		t.Fatal(first, second)
	}
	got, positions, e := repo.GetInstrumentGroups(ctx, tenant, actor)
	if e != nil || len(positions) != 2 || len(got.Memberships) != 2 {
		t.Fatal(got, positions, e)
	}
	p.RequestKey = "stale"
	if _, e = repo.SaveInstrumentGroups(ctx, tenant, actor, p); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal(e)
	}
	p.Version = 1
	p.RequestKey = "invalid-account"
	p.Memberships[0].LedgerID = uuid.NewString()
	if _, e = repo.SaveInstrumentGroups(ctx, tenant, actor, p); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal(e)
	}
	p.Memberships = p.Memberships[1:]
	p.Groups = []nav.InstrumentGroup{{ID: h, Name: "Renamed"}}
	p.RequestKey = "remove"
	got, e = repo.SaveInstrumentGroups(ctx, tenant, actor, p)
	if e != nil || got.Version != 2 || len(got.Memberships) != 1 {
		t.Fatal(got, e)
	}
	var revisions int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_group_revisions WHERE tenant_id=$1`, tenant).Scan(&revisions); e != nil || revisions != 2 {
		t.Fatal(revisions, e)
	}
	var ledgerVersion int64
	if e = pool.DB.QueryRow(ctx, `SELECT version FROM investment_ledgers WHERE id=$1`, accounts[0]).Scan(&ledgerVersion); e != nil || ledgerVersion != 3 {
		t.Fatal("classification modified journal", ledgerVersion, e)
	}
	if _, _, e = repo.GetInstrumentGroups(ctx, uuid.NewString(), actor); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal(e)
	}
	if _, e = pool.DB.Exec(ctx, `UPDATE user_tenants SET role='member' WHERE tenant_id=$1 AND user_id=$2`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	if _, _, e = repo.GetInstrumentGroups(ctx, tenant, actor); e != nil {
		t.Fatal(e)
	}
	p.Version = 2
	p.RequestKey = "member"
	if _, e = repo.SaveInstrumentGroups(ctx, tenant, actor, p); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal(e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0045_investment_groups")
}
