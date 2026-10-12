// Package pgbackup creates complete logical Budget database backups and verifies
// a real restore. Unlike the investment bundle it includes auth and Budget data.
package pgbackup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Capture a bounded prefix only to classify failures. PostgreSQL diagnostics
// can contain private row values; neither the raw text nor object names escape.
type diagnosticPrefix struct{ bytes.Buffer }

func (b *diagnosticPrefix) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := (64 << 10) - b.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}

func toolFailure(stderr string) error {
	switch {
	case strings.Contains(stderr, `schema "public" already exists`):
		return errors.New("PostgreSQL restore conflicts with an existing public schema")
	case strings.Contains(stderr, "permission denied"):
		return errors.New("PostgreSQL tool permission denied")
	case strings.Contains(stderr, "password authentication failed"):
		return errors.New("PostgreSQL tool authentication failed")
	case strings.Contains(stderr, "unsupported version"):
		return errors.New("PostgreSQL tool cannot read this archive version")
	case strings.Contains(stderr, "duplicate key value"):
		return errors.New("PostgreSQL restore encountered duplicate key values")
	case strings.Contains(stderr, "invalid locale"):
		return errors.New("PostgreSQL tool rejected database locale")
	default:
		return errors.New("PostgreSQL tool failed; no database credentials or SQL diagnostics were emitted")
	}
}

type Tools interface {
	Dump(context.Context, string, string, bool, io.Writer) error
	Restore(context.Context, string, io.Reader) error
	Versions(context.Context) (string, string, error)
}
type NativeTools struct{ DumpPath, RestorePath string }

func (n NativeTools) dumpPath() string {
	if n.DumpPath != "" {
		return n.DumpPath
	}
	return "pg_dump"
}
func (n NativeTools) restorePath() string {
	if n.RestorePath != "" {
		return n.RestorePath
	}
	return "pg_restore"
}

// No connection string, password or source row is included in argv or errors.
// libpq receives a private temporary passfile, which is removed after the tool exits.
func toolEnv(dsn string) ([]string, func() error, error) {
	clean := func() error { return nil }
	u, e := url.Parse(dsn)
	if e != nil || u.User == nil || u.Hostname() == "" || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return nil, clean, errors.New("database URL must be a PostgreSQL URI with explicit host and user")
	}
	db, e := url.PathUnescape(strings.TrimPrefix(u.EscapedPath(), "/"))
	if e != nil || db == "" {
		return nil, clean, errors.New("database name is required")
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	if _, e = strconv.ParseUint(port, 10, 16); e != nil {
		return nil, clean, errors.New("invalid PostgreSQL port")
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "LANG=C", "HOME=" + os.Getenv("HOME"), "PGHOST=" + u.Hostname(), "PGPORT=" + port, "PGUSER=" + u.User.Username(), "PGDATABASE=" + db, "PGCONNECT_TIMEOUT=15", "PGAPPNAME=budgetbackup", "PGSSLMODE=prefer"}
	allowed := map[string]string{"sslmode": "PGSSLMODE", "sslrootcert": "PGSSLROOTCERT", "sslcert": "PGSSLCERT", "sslkey": "PGSSLKEY", "connect_timeout": "PGCONNECT_TIMEOUT", "target_session_attrs": "PGTARGETSESSIONATTRS", "channel_binding": "PGCHANNELBINDING", "options": "PGOPTIONS"}
	for key, values := range u.Query() {
		name, ok := allowed[key]
		if !ok || len(values) != 1 {
			return nil, clean, errors.New("unsupported PostgreSQL URL option")
		}
		for i, v := range env {
			if strings.HasPrefix(v, name+"=") {
				env = append(env[:i], env[i+1:]...)
				break
			}
		}
		env = append(env, name+"="+values[0])
	}
	password, hasPassword := u.User.Password()
	if hasPassword {
		f, e := os.CreateTemp("", "budgetbackup-pass-*")
		if e != nil {
			return nil, clean, errors.New("cannot create private PostgreSQL passfile")
		}
		clean = func() error {
			if err := os.Remove(f.Name()); err != nil && !os.IsNotExist(err) {
				return errors.New("cannot remove private PostgreSQL passfile")
			}
			return nil
		}
		escape := func(v string) string { return strings.NewReplacer("\\", "\\\\", ":", "\\:").Replace(v) }
		for _, v := range []string{password, db, u.User.Username(), u.Hostname()} {
			if strings.ContainsAny(v, "\r\n\x00") {
				_ = f.Close() // Failed validation is already the primary error.
				return nil, func() error { return nil }, errors.Join(errors.New("invalid PostgreSQL credential format"), clean())
			}
		}
		_, e = f.WriteString(escape(u.Hostname()) + ":" + port + ":" + escape(db) + ":" + escape(u.User.Username()) + ":" + escape(password) + "\n")
		ce := f.Close()
		if e != nil || ce != nil {
			return nil, func() error { return nil }, errors.Join(errors.New("cannot write private PostgreSQL passfile"), clean())
		}
		env = append(env, "PGPASSFILE="+f.Name())
	}
	return env, clean, nil
}
func runTool(ctx context.Context, path, dsn string, args []string, in io.Reader, out io.Writer) (resultErr error) {
	env, clean, e := toolEnv(dsn)
	if e != nil {
		return e
	}
	defer func() { resultErr = errors.Join(resultErr, clean()) }()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Stdin = in
	cmd.Stdout = out
	diagnostic := &diagnosticPrefix{}
	cmd.Stderr = diagnostic
	defer func() { clear(diagnostic.Bytes()) }()
	if e = cmd.Run(); e != nil {
		return toolFailure(diagnostic.String())
	}
	return nil
}
func (n NativeTools) Dump(ctx context.Context, dsn, snapshot string, schema bool, out io.Writer) error {
	args := []string{"--no-password", "--no-owner", "--no-privileges", "--lock-wait-timeout=15000"}
	if snapshot != "" {
		args = append(args, "--snapshot="+snapshot)
	}
	if schema {
		args = append(args, "--schema-only")
	} else {
		args = append(args, "--format=custom")
	}
	return runTool(ctx, n.dumpPath(), dsn, args, nil, out)
}
func (n NativeTools) Restore(ctx context.Context, dsn string, in io.Reader) error {
	u, e := url.Parse(dsn)
	if e != nil {
		return errors.New("invalid database URL")
	}
	db, e := url.PathUnescape(strings.TrimPrefix(u.EscapedPath(), "/"))
	if e != nil {
		return errors.New("invalid database name")
	}
	return runTool(ctx, n.restorePath(), dsn, []string{"--dbname=" + db, "--no-password", "--single-transaction", "--exit-on-error", "--no-owner", "--no-privileges"}, in, io.Discard)
}
func (n NativeTools) Versions(ctx context.Context) (string, string, error) {
	versions := []string{}
	for _, path := range []string{n.dumpPath(), n.restorePath()} {
		cmd := exec.CommandContext(ctx, path, "--version")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C"}
		out, e := cmd.Output()
		if e != nil || len(out) > 500 {
			return "", "", errors.New("PostgreSQL tool version unavailable")
		}
		versions = append(versions, strings.TrimSpace(string(out)))
	}
	return versions[0], versions[1], nil
}
