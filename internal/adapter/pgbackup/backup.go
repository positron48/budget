package pgbackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"
)

const Format = "budget.postgres.backup.v1"

type Release struct{ BackendImage, WebImage, DatabaseImage, Revision string }
type Manifest struct {
	Format, DumpSHA256, DumpVersion, RestoreVersion string
	StartedAt, CompletedAt                          time.Time
	Release                                         Release
	State                                           State
}
type Report struct {
	ManifestSHA256, DumpSHA256, TargetDatabase string
	StartedAt, CompletedAt                     time.Time
	VerifiedTables                             int
	VerifiedRows                               int64
	State                                      State
	Release                                    Release
}

func sumBytes(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func privateCreate(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}
func writeJSON(path string, v any) (resultErr error) {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return errors.New("cannot encode backup evidence")
	}
	f, e := privateCreate(path)
	if e != nil {
		return errors.New("evidence destination must not exist")
	}
	ok := false
	defer func() {
		_ = f.Close() // Final close is checked below; this also releases failed writes.
		if !ok {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				resultErr = errors.Join(resultErr, errors.New("cannot remove incomplete backup evidence"))
			}
		}
	}()
	if _, e = f.Write(b); e == nil {
		e = flushAndClose(f)
	} else {
		_ = f.Close() // Preserve the failed write; no completion marker is published.
	}
	if e != nil {
		return errors.New("cannot save private backup evidence")
	}
	ok = true
	return nil
}
func validRelease(v Release) bool {
	for _, s := range []string{v.BackendImage, v.WebImage, v.DatabaseImage, v.Revision} {
		if strings.TrimSpace(s) == "" || len(s) > 1024 || strings.ContainsAny(s, "\r\n\x00") {
			return false
		}
	}
	return true
}
func connect(ctx context.Context, dsn string) (*pgx.Conn, error) {
	if _, clean, e := toolEnv(dsn); e != nil {
		return nil, e
	} else {
		if e := clean(); e != nil {
			return nil, e
		}
	}
	c, e := pgx.Connect(ctx, dsn)
	if e != nil {
		return nil, errors.New("database connection failed")
	}
	return c, nil
}
func Backup(ctx context.Context, tools Tools, dsn, dir string, release Release) (manifest Manifest, digest string, resultErr error) {
	var m Manifest
	if !validRelease(release) {
		return m, "", errors.New("backend, web, database image and revision evidence are required")
	}
	if e := os.Mkdir(dir, 0700); e != nil {
		return m, "", errors.New("backup directory must be new")
	}
	ok := false
	defer func() {
		if !ok {
			for _, path := range []string{filepath.Join(dir, "database.dump"), filepath.Join(dir, "manifest.json"), dir} {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					resultErr = errors.Join(resultErr, errors.New("cannot remove incomplete backup artifacts"))
				}
			}
		}
	}()
	m.Format = Format
	m.Release = release
	m.StartedAt = time.Now().UTC()
	var e error
	m.DumpVersion, m.RestoreVersion, e = tools.Versions(ctx)
	if e != nil {
		return m, "", e
	}
	conn, e := connect(ctx, dsn)
	if e != nil {
		return m, "", e
	}
	defer func() { _ = conn.Close(ctx) }()
	var server int
	if e = conn.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&server); e != nil {
		return m, "", errors.New("database version unavailable")
	}
	if e = checkToolVersions(server, m.DumpVersion, m.RestoreVersion); e != nil {
		return m, "", e
	}
	tx, snapshot, e := snapshotTx(ctx, conn)
	if e != nil {
		return m, "", e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	m.State, e = inspect(ctx, tx, tools, dsn, snapshot, "")
	if e != nil {
		return m, "", e
	}
	dump, e := privateCreate(filepath.Join(dir, "database.dump"))
	if e != nil {
		return m, "", errors.New("cannot create private database dump")
	}
	hash := sha256.New()
	e = tools.Dump(ctx, dsn, snapshot, false, io.MultiWriter(dump, hash))
	if e == nil {
		e = flushAndClose(dump)
	} else {
		_ = dump.Close() // Preserve the dump failure and remove incomplete artifacts.
	}
	if e != nil {
		return m, "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return m, "", errors.New("database snapshot transaction failed")
	}
	m.DumpSHA256 = hex.EncodeToString(hash.Sum(nil))
	m.CompletedAt = time.Now().UTC()
	if e = writeJSON(filepath.Join(dir, "manifest.json"), m); e != nil {
		return m, "", e
	}
	// The manifest is the final completion marker; sync the directory as well.
	if e = syncBackupDirectory(dir); e != nil {
		return m, "", e
	}
	raw, e := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if e != nil {
		return m, "", errors.New("cannot read completed manifest")
	}
	ok = true
	return m, sumBytes(raw), nil
}

func syncBackupDirectory(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return errors.New("cannot open backup directory for flush")
	}
	err = flushAndClose(d)
	if err != nil {
		return errors.New("cannot flush backup directory")
	}
	return nil
}

func flushAndClose(file interface {
	Sync() error
	Close() error
}) error {
	flushErr := file.Sync()
	return errors.Join(flushErr, file.Close())
}
func privateRead(path string) (*os.File, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("backup files must be regular private files")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("cannot read backup file")
	}
	return f, nil
}

// Verify checks integrity only. It never claims that a database restore ran.
func Verify(dir, expected string) (Manifest, string, error) {
	var m Manifest
	if info, e := os.Lstat(dir); e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return m, "", errors.New("backup directory must be private and not a symlink")
	}
	f, e := privateRead(filepath.Join(dir, "manifest.json"))
	if e != nil {
		return m, "", e
	}
	raw, e := io.ReadAll(io.LimitReader(f, 8<<20+1))
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil || len(raw) > 8<<20 {
		return m, "", errors.New("invalid backup manifest size")
	}
	sha := sumBytes(raw)
	if expected != "" && sha != expected {
		return m, "", errors.New("backup manifest checksum mismatch")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&m); e != nil || m.Format != Format || len(m.DumpSHA256) != 64 || m.StartedAt.IsZero() || m.CompletedAt.Before(m.StartedAt) || !validRelease(m.Release) || m.State.Migration.Dirty || len(m.State.Tables) == 0 {
		return m, "", errors.New("invalid backup manifest")
	}
	f, e = privateRead(filepath.Join(dir, "database.dump"))
	if e != nil {
		return m, "", e
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	var header [5]byte
	if _, e = io.ReadFull(f, header[:]); e != nil || string(header[:]) != "PGDMP" {
		return m, "", errors.New("not a custom PostgreSQL dump")
	}
	hash.Write(header[:])
	if _, e = io.Copy(hash, f); e != nil {
		return m, "", errors.New("cannot verify database dump")
	}
	if hex.EncodeToString(hash.Sum(nil)) != m.DumpSHA256 {
		return m, "", errors.New("database dump checksum mismatch")
	}
	return m, sha, nil
}

var restoreName = regexp.MustCompile(`^budget_restore_[a-z0-9_]{1,48}$`)

func literal(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func targetDSN(admin, name string) (string, error) {
	u, e := url.Parse(admin)
	if e != nil {
		return "", errors.New("invalid target database URL")
	}
	u.Path = "/" + name
	u.RawPath = ""
	return u.String(), nil
}
func Restore(ctx context.Context, tools Tools, adminDSN, dir, name, expected string) (Report, error) {
	var report Report
	report.StartedAt = time.Now().UTC()
	report.TargetDatabase = name
	if !restoreName.MatchString(name) || len(expected) != 64 {
		return report, errors.New("restore requires a new budget_restore_* database name and reviewed manifest SHA256")
	}
	m, sha, e := Verify(dir, expected)
	if e != nil {
		return report, e
	}
	conn, e := connect(ctx, adminDSN)
	if e != nil {
		return report, e
	}
	defer func() { _ = conn.Close(ctx) }()
	var server int
	if e = conn.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&server); e != nil || server/10000 != m.State.ServerVersion/10000 {
		return report, errors.New("restore requires the same PostgreSQL major version")
	}
	dumpVersion, restoreVersion, e := tools.Versions(ctx)
	if e != nil {
		return report, e
	}
	if e = checkToolVersions(server, dumpVersion, restoreVersion); e != nil {
		return report, e
	}
	var exists bool
	if e = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)`, name).Scan(&exists); e != nil || exists {
		return report, errors.New("restore target must not exist; existing databases are never overwritten")
	}
	settings := m.State.Database
	if settings.LocaleProvider != "c" && settings.LocaleProvider != "i" {
		return report, errors.New("unsupported database locale provider")
	}
	create := `CREATE DATABASE ` + pgx.Identifier{name}.Sanitize() + ` TEMPLATE template0 ENCODING ` + literal(settings.Encoding) + ` LC_COLLATE ` + literal(settings.Collation) + ` LC_CTYPE ` + literal(settings.CType)
	if settings.LocaleProvider == "i" {
		create += ` LOCALE_PROVIDER icu ICU_LOCALE ` + literal(settings.ICULocale)
	} else {
		create += ` LOCALE_PROVIDER libc`
	}
	if _, e = conn.Exec(ctx, create); e != nil {
		return report, errors.New("cannot create new restore database")
	}
	dsn, e := targetDSN(adminDSN, name)
	if e != nil {
		return report, e
	}
	f, e := privateRead(filepath.Join(dir, "database.dump"))
	if e != nil {
		return report, e
	}
	e = tools.Restore(ctx, dsn, f)
	if closeErr := f.Close(); e == nil && closeErr != nil {
		e = errors.New("cannot close restored dump reader")
	}
	if e != nil {
		return report, e
	}
	// Restore has committed as one transaction. A verification failure retains
	// only this new isolated database for diagnosis, never deleting other data.
	target, e := connect(ctx, dsn)
	if e != nil {
		return report, e
	}
	defer func() { _ = target.Close(ctx) }()
	tx, snapshot, e := snapshotTx(ctx, target)
	if e != nil {
		return report, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	actual, e := inspect(ctx, tx, tools, dsn, snapshot, m.State.SchemaSHA256)
	if e != nil {
		return report, e
	}
	expectedState := m.State
	expectedState.ServerVersion = actual.ServerVersion
	if !reflect.DeepEqual(expectedState, actual) {
		return report, errors.New("restored schema, rows, database settings or migration version differ from the backup snapshot")
	}
	if e = tx.Commit(ctx); e != nil {
		return report, errors.New("restore verification transaction failed")
	}
	report.ManifestSHA256 = sha
	report.DumpSHA256 = m.DumpSHA256
	report.State = actual
	report.Release = m.Release
	report.VerifiedTables = len(actual.Tables)
	for _, t := range actual.Tables {
		report.VerifiedRows += t.Rows
	}
	report.CompletedAt = time.Now().UTC()
	return report, nil
}
func SaveReport(path string, report Report) error {
	if report.CompletedAt.IsZero() {
		return errors.New("cannot certify an incomplete restore")
	}
	return writeJSON(path, report)
}
func Summary(m Manifest) string {
	return fmt.Sprintf("schema version %d; %d tables; database snapshot %s", m.State.Migration.Version, len(m.State.Tables), m.StartedAt.Format(time.RFC3339))
}

var toolVersion = regexp.MustCompile(`^pg_(dump|restore) \(PostgreSQL\) (\d+)(?:\.|\s|$)`)

func checkToolVersions(server int, dump, restore string) error {
	major := server / 10000
	if major != 15 && major != 16 {
		return errors.New("backup verification requires PostgreSQL 15 or 16")
	}
	for i, value := range []string{dump, restore} {
		match := toolVersion.FindStringSubmatch(value)
		expectedTool := []string{"dump", "restore"}[i]
		if len(match) != 3 || match[1] != expectedTool || match[2] != fmt.Sprint(major) {
			return errors.New("pg_dump and pg_restore must match the PostgreSQL server major version")
		}
	}
	return nil
}
