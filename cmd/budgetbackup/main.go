package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/positron48/budget/internal/adapter/pgbackup"
	"github.com/positron48/budget/internal/adapter/postgres"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func run() error {
	mode := flag.String("mode", "", "backup, verify, restore or rehearse-upgrade (writes only to a separate new database)")
	from := flag.Int64("upgrade-from", 11, "expected clean source schema for rehearsal")
	to := flag.Int64("upgrade-to", 62, "expected clean target schema for rehearsal")
	dir := flag.String("directory", "", "private backup directory; new for backup, existing for verify/restore")
	expected := flag.String("expected-manifest-sha", "", "reviewed manifest SHA256; required for restore")
	target := flag.String("target-name", "", "new database name beginning budget_restore_")
	report := flag.String("report", "", "new private JSON report path; required for restore")
	backend := flag.String("backend-image", "", "observed backend image/digest; required for backup")
	web := flag.String("web-image", "", "observed web image/digest; required for backup")
	database := flag.String("database-image", "", "observed PostgreSQL image/digest; required for backup")
	revision := flag.String("revision", "", "observed code/GitOps revision; required for backup")
	dumpPath := flag.String("pg-dump", "pg_dump", "PostgreSQL pg_dump executable")
	restorePath := flag.String("pg-restore", "pg_restore", "PostgreSQL pg_restore executable")
	timeout := flag.Duration("timeout", 30*time.Minute, "maximum operation duration")
	flag.Parse()
	if *dir == "" || flag.NArg() != 0 || *timeout <= 0 {
		return errors.New("directory and a positive timeout are required")
	}
	tools := pgbackup.NativeTools{DumpPath: *dumpPath, RestorePath: *restorePath}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	switch *mode {
	case "backup":
		if *target != "" || *report != "" || *expected != "" {
			return errors.New("restore flags cannot be combined with backup")
		}
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			return errors.New("DATABASE_URL is required")
		}
		m, sha, e := pgbackup.Backup(ctx, tools, dsn, *dir, pgbackup.Release{BackendImage: *backend, WebImage: *web, DatabaseImage: *database, Revision: *revision})
		if e != nil {
			return e
		}
		fmt.Printf("Full database backup saved: %s; manifest SHA256 %s. Restore is not yet verified.\n", pgbackup.Summary(m), sha)
	case "verify":
		if *target != "" || *report != "" || *backend != "" || *web != "" || *revision != "" || *database != "" {
			return errors.New("verify cannot be combined with backup/restore flags")
		}
		m, sha, e := pgbackup.Verify(*dir, *expected)
		if e != nil {
			return e
		}
		fmt.Printf("Backup files verified: %s; manifest SHA256 %s. No restore was performed.\n", pgbackup.Summary(m), sha)
	case "restore", "rehearse-upgrade":
		if *target == "" || *report == "" || *backend != "" || *web != "" || *revision != "" || *database != "" {
			return errors.New("restore requires target-name, report and expected-manifest-sha")
		}
		if _, e := os.Lstat(*report); !os.IsNotExist(e) {
			return errors.New("restore report must be a new path")
		}
		dsn := os.Getenv("TARGET_ADMIN_DATABASE_URL")
		if dsn == "" {
			return errors.New("TARGET_ADMIN_DATABASE_URL is required; source DATABASE_URL is not used for restore")
		}
		if *mode == "rehearse-upgrade" {
			result, e := pgbackup.RehearseUpgrade(ctx, tools, dsn, *dir, *target, *expected, *from, *to,
				func(ctx context.Context, cloneDSN string) error {
					if e := ctx.Err(); e != nil {
						return e
					}
					return postgres.RunMigrations(cloneDSN)
				})
			if e != nil {
				return e
			}
			if e = pgbackup.SaveUpgradeReport(*report, result); e != nil {
				return e
			}
			fmt.Printf("Isolated upgrade verified: schema %d -> %d; %d preserved tables, %d rows; repeat startup verified.\n", *from, *to, result.PreservedTables, result.PreservedRows)
			return nil
		}
		result, e := pgbackup.Restore(ctx, tools, dsn, *dir, *target, *expected)
		if e != nil {
			return e
		}
		if e = pgbackup.SaveReport(*report, result); e != nil {
			return e
		}
		fmt.Printf("Restore verified: %d tables, %d rows; schema version %d; elapsed %s; evidence saved.\n", result.VerifiedTables, result.VerifiedRows, result.State.Migration.Version, result.CompletedAt.Sub(result.StartedAt).Round(time.Millisecond))
	default:
		return errors.New("mode must be backup, verify, restore or rehearse-upgrade")
	}
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
