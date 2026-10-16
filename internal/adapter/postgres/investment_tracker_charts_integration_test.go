package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestTrackerChartProjectionSurvivesRestartAndIsBounded(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	basis := strings.Repeat("a", 64)
	for i := 0; i < 30; i++ {
		if err := repo.SaveTrackerChart(ctx, tenant, actor, fmt.Sprintf("%064x", i), basis, []byte("request"), []byte("daily-facts"), true); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_tracker_charts WHERE tenant_id=$1`, tenant).Scan(&n); err != nil || n != 24 {
		t.Fatal("unbounded projections", n, err)
	}
	repo = NewMarketDataRepo(pool)
	key := fmt.Sprintf("%064x", 29)
	raw, err := repo.ReadTrackerChart(ctx, tenant, actor, key, basis)
	if err != nil || string(raw) != "daily-facts" {
		t.Fatal("restart lost projection", err)
	}
	raw, err = repo.ReadTrackerChart(ctx, tenant, actor, key, strings.Repeat("b", 64))
	if err != nil || len(raw) != 0 {
		t.Fatal("stale basis returned", err)
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE investment_tracker_charts SET computed_at=now()-interval '7 hours'`); err != nil {
		t.Fatal(err)
	}
	scopes, err := repo.TrackerChartsToRefresh(ctx)
	if err != nil || len(scopes) != 24 {
		t.Fatal("periodic scopes missing", len(scopes), err)
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE investment_tracker_charts SET computed_at=now()-interval '25 hours'`); err != nil {
		t.Fatal(err)
	}
	raw, err = repo.ReadTrackerChart(ctx, tenant, actor, key, basis)
	if err != nil || len(raw) != 0 {
		t.Fatal("expired projection returned", err)
	}
	// Exercise the exact indexed update/read shape, without touching source facts.
	rows, err := pool.DB.Query(context.Background(), `EXPLAIN UPDATE investment_tracker_charts SET requested_at=now()
 WHERE tenant_id=$1 AND actor_id=$2 AND scope_key=$3 AND basis_key=$4 AND computed_at>now()-interval '24 hours' RETURNING projection`, tenant, actor, key, basis)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		t.Log(line)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}
