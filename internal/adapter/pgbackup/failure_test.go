package pgbackup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingBackupFile struct {
	syncErr, closeErr error
	closed            int
}

func (f *failingBackupFile) Sync() error  { return f.syncErr }
func (f *failingBackupFile) Close() error { f.closed++; return f.closeErr }

func TestBackupFlushAndCloseErrorsRemainFailures(t *testing.T) {
	syncErr, closeErr := errors.New("synthetic sync failure"), errors.New("synthetic close failure")
	for _, pair := range [][2]error{{nil, nil}, {syncErr, nil}, {nil, closeErr}, {syncErr, closeErr}} {
		f := &failingBackupFile{syncErr: pair[0], closeErr: pair[1]}
		err := flushAndClose(f)
		if f.closed != 1 || pair[0] != nil && !errors.Is(err, pair[0]) || pair[1] != nil && !errors.Is(err, pair[1]) || pair[0] == nil && pair[1] == nil && err != nil {
			t.Fatal("flush or close failure lost", f.closed, err)
		}
	}
	dir := t.TempDir()
	if err := syncBackupDirectory(dir); err != nil {
		t.Fatal(err)
	}
	if err := syncBackupDirectory(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing directory flush certified")
	}
}

func TestNativeSuccessfulToolFailsWhenPassfileCleanupFails(t *testing.T) {
	// This fixture deliberately replaces its synthetic passfile with a nonempty
	// directory, making removal fail even when tests run as the file's owner.
	script := filepath.Join(t.TempDir(), "cleanup-fault.sh")
	body := `#!/bin/sh
set -eu
printf '%s' "$PGPASSFILE"
rm "$PGPASSFILE"
mkdir "$PGPASSFILE"
printf x > "$PGPASSFILE/obstacle"
`
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := runTool(context.Background(), script, "postgres://fixture:sensitive-fixture@localhost/budget", nil, nil, &output)
	path := output.String()
	if filepath.Dir(path) != filepath.Clean(os.TempDir()) || !strings.HasPrefix(filepath.Base(path), "budgetbackup-pass-") {
		t.Fatal("unexpected synthetic passfile location")
	}
	defer func() {
		if e := os.Remove(filepath.Join(path, "obstacle")); e != nil {
			t.Error("cannot remove fixture obstacle")
		}
		if e := os.Remove(path); e != nil {
			t.Error("cannot remove fixture directory")
		}
	}()
	if err == nil || !strings.Contains(err.Error(), "cannot remove private PostgreSQL passfile") {
		t.Fatal("successful process hid cleanup failure", err)
	}
	if strings.Contains(err.Error(), "sensitive-fixture") || strings.Contains(err.Error(), path) {
		t.Fatal("cleanup error exposed credential or path")
	}
}
