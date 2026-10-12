package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	md "github.com/positron48/budget/internal/usecase/marketdata"
	"github.com/positron48/budget/internal/usecase/workerhealth"
)

func TestInvestmentWorkerHealthIsolationBackoffAndLoad(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, other, actor string
	for _, v := range []*string{&tenant, &other} {
		if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Worker health synthetic','RUB') RETURNING id`).Scan(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('worker-health@example.invalid','synthetic') RETURNING id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	repo := NewMarketDataRepo(pool)
	if err := repo.ConfigureSource(ctx, scope, "Synthetic source", true); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,status,available_at,lease_token,lease_until)
 SELECT $1,'tinvest',$2,md5(i::text)::uuid,'last_price','2020-01-01','2020-01-01',status,
 statement_timestamp()+CASE WHEN i=2 THEN interval '1 hour' ELSE interval '-10 minutes' END,
 CASE WHEN status='running' THEN gen_random_uuid() END,
 CASE WHEN i=3 THEN statement_timestamp()+interval '1 hour' WHEN i=4 THEN statement_timestamp()-interval '5 minutes' END
 FROM (VALUES(1,'pending'),(2,'pending'),(3,'running'),(4,'running'),(5,'error'),(6,'ready')) AS s(i,status)`
	for _, args := range [][]any{{tenant, "personal"}, {tenant, "different-entitlement"}, {other, "personal"}} {
		if _, err := pool.DB.Exec(ctx, insert, args...); err != nil {
			t.Fatal(err)
		}
	}
	check := func(s workerhealth.Snapshot) {
		t.Helper()
		byStatus := map[string]workerhealth.Queue{}
		for _, q := range s.Queues {
			byStatus[q.Status] = q
		}
		if len(byStatus) != 4 || byStatus["pending"].Count != 2 || byStatus["pending"].Due != 1 || byStatus["pending"].OldestDueSeconds < 600 || byStatus["running"].Count != 2 || byStatus["running"].Due != 1 || byStatus["running"].ExpiredLeases != 1 || byStatus["running"].OldestDueSeconds < 300 || byStatus["error"].Due != 0 || byStatus["ready"].OldestDueSeconds != 0 {
			t.Fatal("scope, backoff or lease classification", s)
		}
	}
	s, err := repo.ReadMarketWorkerHealth(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	check(s)
	if s.SourceEnabled == nil || !*s.SourceEnabled {
		t.Fatal("source enablement missing")
	}
	if err = repo.ConfigureSource(ctx, scope, "Synthetic source", false); err != nil {
		t.Fatal(err)
	}
	s, err = repo.ReadMarketWorkerHealth(ctx, scope)
	if err != nil || s.SourceEnabled == nil || *s.SourceEnabled {
		t.Fatal(s, err)
	}
	check(s) // Disabling a source must preserve diagnostics and queue history.
	if _, err = pool.DB.Exec(ctx, `INSERT INTO investment_nav_jobs(tenant_id,actor_id,series_key,request,report_date,available_at) VALUES($1,$2,'synthetic','{}','2020-01-01',statement_timestamp()-interval '10 minutes'),($3,$2,'synthetic','{}','2020-01-01',statement_timestamp()-interval '10 minutes')`, tenant, actor, other); err != nil {
		t.Fatal(err)
	}
	nav, err := repo.ReadNAVWorkerHealth(ctx, tenant)
	if err != nil || len(nav.Queues) != 1 || nav.Queues[0].Kind != "daily_nav" || nav.Queues[0].Count != 1 || nav.Queues[0].Due != 1 {
		t.Fatal(nav, err)
	}
	if _, err = repo.ReadNAVWorkerHealth(ctx, "invalid"); err == nil {
		t.Fatal("invalid tenant accepted")
	}
	if _, err = pool.DB.Exec(ctx, `INSERT INTO user_tenants(user_id,tenant_id,role) VALUES($1,$2,'owner')`, actor, tenant); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, c := range []string{"a", "b", "c", "d", "e", "f", "1"} {
		key := strings.Repeat(c, 64)
		keys = append(keys, key)
		date := "2020-01-01"
		if c == "1" {
			date = "2040-01-01"
		}
		if _, err = pool.DB.Exec(ctx, `INSERT INTO investment_nav_watches(tenant_id,actor_id,series_key,request,from_date,to_date,enabled) VALUES($1,$2,$3,'{}',$4::date,$4::date,$5)`, tenant, actor, key, date, c != "e"); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.DB.Exec(ctx, `INSERT INTO investment_nav_series(tenant_id,series_key,definition) VALUES($1,$2,'{}')`, tenant, key); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.DB.Exec(ctx, `INSERT INTO investment_portfolios(tenant_id,actor_id,version,data) VALUES($1,$2,1,jsonb_build_object('SeriesKey',$3::text,'AutoExtend',true))`, tenant, actor, keys[5]); err != nil {
		t.Fatal(err)
	}
	var generation int64
	if err = pool.DB.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&generation); err != nil || generation < 2 {
		t.Fatal(generation, err)
	}
	for i := range 3 {
		gen := generation
		if i == 2 {
			gen = 1
		}
		if _, err = pool.DB.Exec(ctx, `INSERT INTO investment_daily_nav(tenant_id,series_key,report_date,generation,data,payload_hash) VALUES($1,$2,'2020-01-01',$3,jsonb_build_object('Complete',$4::boolean),'synthetic')`, tenant, keys[i], gen, i != 1); err != nil {
			t.Fatal(err)
		}
	}
	nav, err = repo.ReadNAVWorkerHealth(ctx, tenant)
	if err != nil || !reflect.DeepEqual(nav.NAVStates, map[string]int64{"ready": 1, "incomplete": 1, "stale": 1, "unavailable": 2}) {
		t.Fatal("watched endpoints, disabled/future exclusion, automatic extension", nav, err)
	}
	// Large terminal history in this scope and another entitlement. No financial
	// journal operations, credential or provider requests are needed for this load.
	if _, err = pool.DB.Exec(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,status)
 SELECT $1,'tinvest',CASE WHEN i<=100000 THEN 'personal' ELSE 'different-entitlement' END,gen_random_uuid(),'last_price','2020-01-01','2020-01-01','ready' FROM generate_series(1,200000) AS i`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, `ANALYZE market_data_jobs`); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	started := time.Now()
	s, err = repo.ReadMarketWorkerHealth(bounded, scope)
	if err != nil {
		t.Fatal(err)
	}
	check(s)
	for _, q := range s.Queues {
		if q.Status == "ready" && q.Count != 100001 {
			t.Fatal("history scope leak", q)
		}
	}
	t.Logf("100006 scoped jobs / 200018 total: snapshot=%s", time.Since(started))
	for _, query := range []struct {
		sql  string
		args []any
	}{
		{marketWorkerHealthSQL, []any{tenant, "tinvest", "personal"}},
		{navWorkerHealthSQL, []any{tenant}},
	} {
		var raw []byte
		if err = pool.DB.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query.sql, query.args...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var plan []map[string]any
		if err = json.Unmarshal(raw, &plan); err != nil || len(plan) != 1 {
			t.Fatal("invalid plan", err)
		}
		t.Logf("read-only exact query EXPLAIN: %s", raw)
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if _, err = repo.ReadMarketWorkerHealth(cancelled, scope); err == nil {
		t.Fatal("cancelled read accepted")
	}
}
