package postgres

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/positron48/budget/migrations"
)

func TestInvestmentEmptyRollback(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		// Roll back the actual post-assets release chain, not a stale copied list.
		if !entry.IsDir() && entry.Name() >= "0012_" && strings.HasSuffix(entry.Name(), ".down.sql") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("investment rollback migrations missing")
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names {
		sql, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.DB.Exec(ctx, string(sql)); err != nil {
			t.Fatal(name, err)
		}
	}
	applyMigrations(t, ctx, pool.DB)
}

func assertInvestmentRollbackRefused(t *testing.T, ctx context.Context, pool *Pool, name string) {
	t.Helper()
	sql, err := migrations.FS.ReadFile(name + ".down.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.DB.Exec(ctx, string(sql))
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "55000" {
		t.Fatalf("%s: expected explicit data-preservation guard, got %v", name, err)
	}
}
