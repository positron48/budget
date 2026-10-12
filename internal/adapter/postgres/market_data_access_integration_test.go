package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestMarketSourceAccessAndRefreshLimits(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, foreign, user string
	for _, id := range []*string{&tenant, &foreign} {
		if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Market access','RUB') RETURNING id`).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('market-access@example.test','test') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'member')`, tenant, user); err != nil {
		t.Fatal(err)
	}
	repo := NewMarketDataRepo(pool)
	svc := md.NewService(repo, NewTenantRepo(pool))
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	q := md.JobRequest{InstrumentID: uuid.NewString(), Kind: "last_price"}
	if _, err := svc.Refresh(ctx, tenant, user, scope, q); !errors.Is(err, md.ErrNotFound) {
		t.Fatal("unprovisioned source accepted", err)
	}
	if err := repo.ConfigureSource(ctx, scope, "T-Invest", true); err != nil {
		t.Fatal(err)
	}
	sources, err := svc.Sources(ctx, tenant, user)
	if err != nil || len(sources) != 1 || sources[0].Title != "T-Invest" {
		t.Fatal(sources, err)
	}
	if _, err = svc.Sources(ctx, foreign, user); !errors.Is(err, md.ErrForbidden) {
		t.Fatal("foreign tenant accepted", err)
	}
	other := scope
	other.TenantID = foreign
	if _, err = svc.Refresh(ctx, tenant, user, other, q); !errors.Is(err, md.ErrForbidden) {
		t.Fatal("scope changed tenant", err)
	}
	first, err := svc.Refresh(ctx, tenant, user, scope, q)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := svc.Refresh(ctx, tenant, user, scope, q)
	if err != nil || repeated.ID != first.ID {
		t.Fatal("duplicate refresh", repeated, err)
	}
	job, err := svc.Job(ctx, tenant, user, first.ID)
	if err != nil || job.Status != "pending" || job.LeaseToken != "" {
		t.Fatal(job, err)
	}
	if _, err = repo.JobStatus(ctx, foreign, first.ID); !errors.Is(err, md.ErrNotFound) {
		t.Fatal("job ID tenant leak", err)
	}
	active, err := repo.Claim(ctx, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	observation := md.Observation{InstrumentID: q.InstrumentID, Kind: q.Kind, At: at, SourceKind: "exchange", Price: "9007199254740993.000000001", PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Complete: true}
	if err = repo.Complete(ctx, active, []md.Observation{observation}); err != nil {
		t.Fatal(err)
	}
	again, err := svc.Refresh(ctx, tenant, user, scope, q)
	if err != nil || again.ID != first.ID {
		t.Fatal("ready cooldown bypass", again, err)
	}
	history, err := svc.History(ctx, tenant, user, scope, q.InstrumentID, q.Kind, at, at)
	if err != nil || len(history) != 1 || history[0].Price != observation.Price {
		t.Fatal(history, err)
	}
	for i := 0; i < 9; i++ {
		request := q
		request.InstrumentID = uuid.NewString()
		if _, err = svc.Refresh(ctx, tenant, user, scope, request); err != nil {
			t.Fatal(err)
		}
	}
	another := q
	another.InstrumentID = uuid.NewString()
	if _, err = svc.Refresh(ctx, tenant, user, scope, another); !errors.Is(err, md.ErrLimit) {
		t.Fatal("minute quota bypass", err)
	}
	if _, err = svc.Refresh(ctx, tenant, user, scope, q); err != nil {
		t.Fatal("quota blocked existing request", err)
	}
	// Independent active-job cap after resetting the one-minute window.
	for i := 0; i < 91; i++ {
		request := q
		request.InstrumentID = uuid.NewString()
		if _, err = repo.Enqueue(ctx, scope, request); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE market_data_jobs SET created_at=clock_timestamp()-interval '2 minutes' WHERE tenant_id=$1`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Refresh(ctx, tenant, user, scope, another); !errors.Is(err, md.ErrLimit) {
		t.Fatal("active quota bypass", err)
	}
	if err = repo.ConfigureSource(ctx, scope, "T-Invest", false); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.History(ctx, tenant, user, scope, q.InstrumentID, q.Kind, at, at); !errors.Is(err, md.ErrNotFound) {
		t.Fatal("disabled source history leak", err)
	}
	if _, err = svc.Job(ctx, tenant, user, first.ID); !errors.Is(err, md.ErrNotFound) {
		t.Fatal("disabled source job leak", err)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0022_market_sources")
}
