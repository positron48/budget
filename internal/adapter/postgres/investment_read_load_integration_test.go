package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

// Opt-in scale acceptance, never a production DSN. Cached projections are seeded
// directly to isolate warm reads; this is not a 10-year financial replay test.
func TestInvestmentWarmReadScale(t *testing.T) {
	if os.Getenv("INVESTMENT_READ_LOAD") != "1" {
		t.Skip("set INVESTMENT_READ_LOAD=1 for scale acceptance")
	}
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Read load','RUB') RETURNING id`).Scan(&tenant))
	must(pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('load@example.test','test') RETURNING id`).Scan(&actor))
	_, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor)
	must(err)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	start := time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)
	q := nav.Request{TenantID: tenant, UserID: actor, Currency: "RUB", FXPolicyKey: "market"}
	for i := 0; i < 10; i++ {
		l, e := svc.CreateLedger(ctx, tenant, actor, fmt.Sprintf("Ledger %d", i), start, fmt.Sprintf("ledger-%d", i))
		must(e)
		q.LedgerIDs = append(q.LedgerIDs, l.ID)
	}
	rows := make([][]any, 0, 100000)
	for i := 0; i < 100000; i++ {
		ledger := q.LedgerIDs[i%10]
		at := start.AddDate(0, 0, 1+i*3651/100000)
		op := d.InvestmentOperation{ID: uuid.NewString(), TenantID: tenant, LedgerID: ledger, Kind: "buy", OccurredAt: at, SettlementAt: at, Sequence: int64(i/10 + 1), Version: 1, Status: "posted", InstrumentKey: fmt.Sprintf("stock-%03d", i%500), Currency: "RUB", Quantity: "1", Amount: "1"}
		raw, e := json.Marshal(op)
		must(e)
		rows = append(rows, []any{op.ID, tenant, ledger, op.Kind, at, at, op.Sequence, int64(1), op.Status, raw})
	}
	_, err = pool.DB.CopyFrom(ctx, pgx.Identifier{"investment_operations"}, []string{"id", "tenant_id", "ledger_id", "kind", "occurred_at", "settlement_at", "sequence", "version", "status", "data"}, pgx.CopyFromRows(rows))
	must(err)
	key, definition, e := navSeries(q)
	must(e)
	request, e := json.Marshal(q)
	must(e)
	_, err = pool.DB.Exec(ctx, `INSERT INTO investment_nav_series(tenant_id,series_key,definition) VALUES($1,$2,$3)`, tenant, key, definition)
	must(err)
	_, err = pool.DB.Exec(ctx, `INSERT INTO investment_nav_watches(tenant_id,actor_id,series_key,request,from_date,to_date) VALUES($1,$2,$3,$4,'2015-01-02','2024-12-31')`, tenant, actor, key, request)
	must(err)
	var generation int64
	must(pool.DB.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&generation))
	result := nav.Result{TenantID: tenant, Currency: "RUB", FXPolicy: "market", DataGeneration: generation, Components: map[string]nav.ComponentIdentity{}, PortfolioValue: calc.PortfolioValue{Valuation: calc.Valuation{Complete: true, KnownValue: big.NewRat(100000, 1), ComponentValues: map[string]*big.Rat{}}}}
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("position-%d", i)
		result.Components[id] = nav.ComponentIdentity{LedgerID: q.LedgerIDs[i%10], Kind: "position", Key: fmt.Sprintf("stock-%03d", i), Currency: "RUB", Quantity: "200"}
		result.ComponentValues[id] = big.NewRat(200, 1)
	}
	raw, e := json.Marshal(result)
	must(e)
	_, err = pool.DB.Exec(ctx, `INSERT INTO investment_daily_nav(tenant_id,series_key,report_date,generation,data,payload_hash) SELECT $1,$2,d::date,$3,$4::jsonb || jsonb_build_object('At',to_char(d + interval '21 hours' - interval '1 microsecond','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),'synthetic-load' FROM generate_series('2015-01-01'::timestamp,'2024-12-31'::timestamp,interval '1 day') d`, tenant, key, generation, raw)
	must(err)
	_, err = pool.DB.Exec(ctx, `ANALYZE investment_operations`)
	must(err)
	t.Log("fixture ready: 10 ledgers, 500 instruments, 100000 operations, 3653 cached daily projections")
	repo := NewMarketDataRepo(pool)
	measure := func(name string, limit time.Duration, fn func() error) {
		t.Helper()
		must(fn())
		samples := make([]time.Duration, 20)
		for i := range samples {
			at := time.Now()
			must(fn())
			samples[i] = time.Since(at)
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		p95 := samples[18]
		t.Logf("%s samples=20 p50=%s p95=%s max=%s target=%s", name, samples[9], p95, samples[19], limit)
		if p95 >= limit {
			t.Errorf("%s exceeds warm read target: %s >= %s", name, p95, limit)
		}
	}

	planTx, e := pool.DB.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	must(e)
	after, _ := nav.DayCutoff("2024-11-30")
	through, _ := nav.DayCutoff("2024-12-31")
	planRows, e := planTx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+investmentOperationsPeriodSQL, tenant, q.LedgerIDs[0], after, through, inv.MaxOperations+1)
	must(e)
	for planRows.Next() {
		var line string
		must(planRows.Scan(&line))
		t.Log("period SQL:", line)
	}
	must(planRows.Err())
	planRows.Close()
	must(planTx.Rollback(ctx))
	measure("overview", time.Second, func() error {
		v, e := repo.OverviewForSeries(ctx, tenant, actor, key, "2024-12-01", "2024-12-31")
		if e == nil && (v.NAV.Result == nil || v.NAV.Result.KnownValue.Cmp(big.NewRat(100000, 1)) != 0) {
			return fmt.Errorf("overview value mismatch")
		}
		return e
	})
	measure("ten-year-performance", 2*time.Second, func() error {
		v, e := repo.PerformanceForSeries(ctx, tenant, actor, key, "2015-01-02", "2024-12-31")
		if e == nil && (len(v.Days) != 3653 || v.Profit == nil || v.Profit.Sign() != 0 || v.TWR.Return == nil || v.TWR.Return.Sign() != 0) {
			return fmt.Errorf("performance coverage or value mismatch")
		}
		return e
	})
	measure("ten-year-chart", 2*time.Second, func() error {
		v, e := repo.NAVHistoryForSeries(ctx, tenant, actor, key, "2015-01-02", "2024-12-31")
		if e == nil && (len(v) != 3652 || v[len(v)-1].State != "ready") {
			return fmt.Errorf("chart coverage mismatch: %d", len(v))
		}

		if e == nil {
			for _, point := range v {
				if point.State != "ready" || point.Result == nil || point.Result.KnownValue == nil || point.Result.KnownValue.Cmp(big.NewRat(100000, 1)) != 0 {
					return fmt.Errorf("chart point mismatch: %s", point.Date)
				}
			}
		}
		return e
	})
}
