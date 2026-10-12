package pgbackup_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/positron48/budget/internal/adapter/pgbackup"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestUpgradeRehearsalRejectsLossAndUnsafeTargets(t *testing.T) {
	if os.Getenv("PG_INTEGRATION") != "1" {
		t.Skip("set PG_INTEGRATION=1")
	}
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:15-alpine", tcpostgres.WithDatabase("budget"), tcpostgres.WithUsername("budget"), tcpostgres.WithPassword("budget"), tcpostgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = container.Terminate(ctx) }()
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	source, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close(ctx) }()
	_, err = source.Exec(ctx, `CREATE TABLE schema_migrations(version bigint PRIMARY KEY,dirty boolean NOT NULL);INSERT INTO schema_migrations VALUES(11,false);CREATE TABLE fixture(id bigint,payload text);INSERT INTO fixture VALUES(9007199254740993,'private-original'),(9007199254740993,'private-original')`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	tool := func(name string) string {
		path := filepath.Join(dir, name)
		body := "#!/bin/sh\nexec " + dockerPath + " exec -i -e PGHOST=/var/run/postgresql -e PGPORT=5432 -e PGUSER -e PGDATABASE -e PGSSLMODE=disable " + container.GetContainerID() + " " + name + " \"$@\"\n"
		if err := os.WriteFile(path, []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	tools := pgbackup.NativeTools{DumpPath: tool("pg_dump"), RestorePath: tool("pg_restore")}
	backupDir := filepath.Join(dir, "backup")
	release := pgbackup.Release{BackendImage: "fixture/backend@sha256:" + strings.Repeat("1", 64), WebImage: "fixture/web@sha256:" + strings.Repeat("2", 64), DatabaseImage: "fixture/postgres@sha256:" + strings.Repeat("3", 64), Revision: "fixture"}
	_, sha, err := pgbackup.Backup(ctx, tools, dsn, backupDir, release)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, sql, want string
		secondOnly      bool
	}{
		{"delete", "DELETE FROM fixture", "upgrade changed legacy rows", false},
		{"update", "UPDATE fixture SET payload='private-changed'", "upgrade changed legacy rows", false},
		{"drop_column", "ALTER TABLE fixture DROP COLUMN payload", "upgrade changed or removed legacy columns", false},
		{"type", "ALTER TABLE fixture ALTER COLUMN id TYPE numeric", "upgrade changed or removed legacy columns", false},
		{"repeat", "CREATE TABLE unexpected(id int)", "repeat startup changed", true},
		{"error", "", "SQL diagnostics suppressed", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			called := 0
			migrate := func(ctx context.Context, clone string) error {
				called++
				u, _ := url.Parse(clone)
				if u.Path == "/budget" {
					t.Fatal("source passed to migrator")
				}
				if test.name == "error" {
					return errors.New("private-secret-SQL")
				}
				conn, err := pgx.Connect(ctx, clone)
				if err != nil {
					return err
				}
				defer func() { _ = conn.Close(ctx) }()
				_, err = conn.Exec(ctx, `UPDATE schema_migrations SET version=12;ALTER TABLE fixture ADD COLUMN IF NOT EXISTS additive text`)
				if err != nil {
					return err
				}
				if !test.secondOnly || called == 2 {
					_, err = conn.Exec(ctx, test.sql)
				}
				return err
			}
			report, err := pgbackup.RehearseUpgrade(ctx, tools, dsn, backupDir, "budget_restore_loss_"+test.name, sha, 11, 12, migrate)
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("unexpected error: %v", err)
			}
			if err = pgbackup.SaveUpgradeReport(filepath.Join(dir, test.name+".json"), report); err == nil {
				t.Fatal("failed upgrade certified")
			}
		})
	}
	called := false
	_, err = pgbackup.RehearseUpgrade(ctx, tools, dsn, backupDir, "budget", sha, 11, 12, func(context.Context, string) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("unsafe target accepted")
	}
	_, err = pgbackup.RehearseUpgrade(ctx, tools, dsn, backupDir, "budget_restore_loss_delete", sha, 11, 12, func(context.Context, string) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("existing target accepted")
	}
	var value string
	var count, version int
	if err = source.QueryRow(ctx, `SELECT min(payload),count(*) FROM fixture`).Scan(&value, &count); err != nil {
		t.Fatal(err)
	}
	if err = source.QueryRow(ctx, `SELECT version FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if value != "private-original" || count != 2 || version != 11 {
		t.Fatal("source changed")
	}
}
