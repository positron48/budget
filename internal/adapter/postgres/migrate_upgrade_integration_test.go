package postgres

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/positron48/budget/migrations"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Exercise the tracked startup path from the production version, rather than
// replaying SQL into an empty latest schema. All fixture credentials are synthetic.
func TestMigrationUpgradeFromProduction11_PG(t *testing.T) {
	if os.Getenv("PG_INTEGRATION") != "1" {
		t.Skip("set PG_INTEGRATION=1")
	}
	ctx := context.Background()
	c, err := tcpostgres.Run(ctx, "postgres:15-alpine", tcpostgres.WithDatabase("budget"), tcpostgres.WithUsername("budget"), tcpostgres.WithPassword("budget"), tcpostgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(11); err != nil {
		t.Fatal(err)
	}
	se, de := m.Close()
	if se != nil || de != nil {
		t.Fatal(se, de)
	}
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.DB.Exec(ctx, `
INSERT INTO tenants(id,name,default_currency_code) VALUES ('00000000-0000-0000-0000-000000000001','Legacy tenant','RUB');
INSERT INTO users(id,email,password_hash,name) VALUES ('00000000-0000-0000-0000-000000000002','upgrade@example.test','synthetic-hash','Legacy user');
INSERT INTO user_tenants(user_id,tenant_id,role) VALUES ('00000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001','owner');
INSERT INTO categories(id,tenant_id,kind,code) VALUES ('00000000-0000-0000-0000-000000000003','00000000-0000-0000-0000-000000000001','expense','legacy');
INSERT INTO transactions(tenant_id,user_id,category_id,type,amount_numeric,currency_code,base_amount_numeric,base_currency_code,occurred_at,comment,is_extraordinary) VALUES ('00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000003','expense',123.45,'RUB',123.45,'RUB','2026-10-01T12:00:00Z','Legacy operation',true);
INSERT INTO asset_accounts(id,tenant_id,name,kind,fixed_currency_code) VALUES
('00000000-0000-0000-0000-000000000004','00000000-0000-0000-0000-000000000001','Legacy property','property','RUB'),
('00000000-0000-0000-0000-000000000005','00000000-0000-0000-0000-000000000001','Legacy investment valuation','investment','RUB');
INSERT INTO asset_snapshots(tenant_id,account_id,currency_code,amount_minor,as_of,kind,user_id) SELECT tenant_id,id,'RUB',9007199254740993,'2026-10-01T12:00:00Z','valuation','00000000-0000-0000-0000-000000000002' FROM asset_accounts;
`)
	if err != nil {
		t.Fatal(err)
	}
	// Preserve every original column of every original table, including empty
	// tables. Additive new columns are deliberately excluded from this comparison.
	rows, err := pool.DB.Query(ctx, `SELECT table_name, column_name FROM information_schema.columns WHERE table_schema='public' AND table_name <> 'schema_migrations' ORDER BY table_name,ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	columns := map[string][]string{}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		columns[table] = append(columns[table], pgx.Identifier{column}.Sanitize())
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	snapshot := func() map[string]string {
		result := map[string]string{}
		for table, cols := range columns {
			q := "SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]'::jsonb)::text FROM (SELECT " + strings.Join(cols, ",") + " FROM " + pgx.Identifier{"public", table}.Sanitize() + ") x"
			var value string
			if err := pool.DB.QueryRow(ctx, q).Scan(&value); err != nil {
				t.Fatal(err)
			}
			result[table] = value
		}
		return result
	}
	before := snapshot()
	legacyInit, err := migrations.FS.ReadFile("0007_add_transaction_extraordinary.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for attempt := range 2 {
		// The applied GitOps revision still runs this idempotent legacy init SQL
		// before each backend start; it does not track the newer migrations.
		if _, err := pool.DB.Exec(ctx, string(legacyInit)); err != nil {
			t.Fatal(err)
		}
		if err := RunMigrations(dsn); err != nil {
			t.Fatal(err)
		}
		if after := snapshot(); !reflect.DeepEqual(before, after) {
			t.Fatalf("legacy data changed on startup %d", attempt+1)
		}
		var version int
		var dirty bool
		if err := pool.DB.QueryRow(ctx, `SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
			t.Fatal(err)
		}
		if version != 64 || dirty {
			t.Fatalf("migration state: version=%d dirty=%t", version, dirty)
		}
	}
	// Reproduce the previous binary's actual migration startup against schema 64.
	oldFS := fstest.MapFS{}
	files, e := migrations.FS.ReadDir(".")
	if e != nil {
		t.Fatal(e)
	}
	for _, file := range files {
		if file.IsDir() || len(file.Name()) < 4 {
			continue
		}
		version, e := strconv.Atoi(file.Name()[:4])
		if e != nil || version > 63 {
			continue
		}
		raw, e := migrations.FS.ReadFile(file.Name())
		if e != nil {
			t.Fatal(e)
		}
		oldFS[file.Name()] = &fstest.MapFile{Data: raw}
	}
	oldSource, e := iofs.New(oldFS, ".")
	if e != nil {
		t.Fatal(e)
	}
	oldDB, e := sql.Open("pgx", dsn)
	if e != nil {
		t.Fatal(e)
	}
	oldDriver, e := migratepgx.WithInstance(oldDB, &migratepgx.Config{})
	if e != nil {
		t.Fatal(e)
	}
	oldMigrate, e := migrate.NewWithInstance("iofs", oldSource, "pgx5", oldDriver)
	if e != nil {
		t.Fatal(e)
	}
	if e = oldMigrate.Up(); e == nil || !strings.Contains(e.Error(), "64") {
		t.Fatal("old binary accepted native schema", e)
	}
	_, _ = oldMigrate.Close()
	var current int
	var dirty bool
	if e = pool.DB.QueryRow(ctx, `SELECT version,dirty FROM schema_migrations`).Scan(&current, &dirty); e != nil || current != 64 || dirty {
		t.Fatal("old startup altered current schema", current, dirty, e)
	}
	t.Logf("tracked upgrade 11 -> 64 and restart preserved %d legacy tables", len(before))
}
