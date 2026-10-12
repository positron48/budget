package pgbackup

import (
	"os"
	"strings"
	"testing"
)

func TestToolFailureClassifiesWithoutPrivateDiagnostics(t *testing.T) {
	for _, sample := range []struct{ raw, want string }{
		{`pg_restore: ERROR: schema "public" already exists; secret-value`, "existing public schema"},
		{"ERROR: permission denied for private-table secret-value", "permission denied"},
		{"ERROR: password authentication failed for user secret-value", "authentication failed"},
		{"ERROR: duplicate key value secret-value", "duplicate key values"},
		{"unknown diagnostic containing secret-value", "no database credentials or SQL diagnostics"},
	} {
		err := toolFailure(sample.raw)
		if strings.Contains(err.Error(), "secret-value") || !strings.Contains(err.Error(), sample.want) {
			t.Fatalf("unsafe or wrong error: %v", err)
		}
	}
	b := &diagnosticPrefix{}
	raw := []byte(strings.Repeat("x", 100<<10))
	if n, err := b.Write(raw); err != nil || n != len(raw) || b.Len() != 64<<10 {
		t.Fatalf("diagnostic bound: n=%d len=%d err=%v", n, b.Len(), err)
	}
}

func TestImplicitPublicArchiveHeaderDoesNotMaskSchemaSQL(t *testing.T) {
	base := "SET row_security = off;\n\n--\n-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: budget\n--\nCREATE EXTENSION IF NOT EXISTS pgcrypto;\n"
	legacy := "SET row_security = off;\n\n--\n-- Name: public; Type: SCHEMA; Schema: -; Owner: budget\n--\n\n-- *not* creating schema, since initdb creates it\n\n\n--\n-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: budget\n--\nCREATE EXTENSION IF NOT EXISTS pgcrypto;\n"
	if string(normalizeSchema([]byte(base))) != string(normalizeSchema([]byte(legacy))) {
		t.Fatal("implicit public header treated as changed schema")
	}
	if string(legacyPublicSchema(normalizeSchema([]byte(base)))) != strings.TrimSpace(strings.ReplaceAll(legacy, "Owner: budget", "Owner: <restore-owner>")) {
		t.Fatal("legacy manifest representation differs")
	}
	for _, sql := range []string{"CREATE SCHEMA public;", "COMMENT ON SCHEMA public IS 'private source comment';", "CREATE FUNCTION public.fixture() RETURNS text LANGUAGE sql AS $$SELECT 'private source text'$$;"} {
		if string(normalizeSchema([]byte(base+sql))) == string(normalizeSchema([]byte(base))) {
			t.Fatal("actual schema SQL hidden")
		}
	}
}

func TestNativeConnectionDoesNotExposeCredentials(t *testing.T) {
	env, clean, e := toolEnv("postgres://fixture:p%3Aa%5Css@localhost:5432/budget?sslmode=verify-full&sslrootcert=%2Ftmp%2Fca.pem")
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if err := clean(); err != nil {
			t.Error(err)
		}
	}()
	var path string
	for _, v := range env {
		if strings.Contains(v, "p:a\\ss") || strings.HasPrefix(v, "PGPASSWORD=") {
			t.Fatal("password exposed in child environment")
		}
		if strings.HasPrefix(v, "PGPASSFILE=") {
			path = strings.TrimPrefix(v, "PGPASSFILE=")
		}
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("passfile permissions", e)
	}
	raw, e := os.ReadFile(path)
	if e != nil || string(raw) != "localhost:5432:budget:fixture:p\\:a\\\\ss\n" {
		t.Fatal("passfile encoding")
	}
	if err := clean(); err != nil {
		t.Fatal(err)
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("passfile not removed")
	}
	for _, dsn := range []string{"password=unsafe", "postgres://fixture:line%0Abreak@localhost/budget", "postgres://fixture@localhost/budget?sslmode=verify-full&unknown=discard"} {
		_, clean, e := toolEnv(dsn)
		if err := clean(); err != nil {
			t.Fatal(err)
		}
		if e == nil {
			t.Fatal("unsupported configuration accepted")
		}
	}
}
func TestSchemaNormalizationPreservesDDL(t *testing.T) {
	raw := []byte("-- Dumped from database version 16.1\n\\restrict random\nCREATE TABLE facts(id int);\nCOMMENT ON TABLE facts IS 'important';\n\\unrestrict random\n")
	got := string(normalizeSchema(raw))
	if got != "CREATE TABLE facts(id int);\nCOMMENT ON TABLE facts IS 'important';" {
		t.Fatal(got)
	}
}

func TestBackupToolVersionsMatchSupportedServerMajorAndCommand(t *testing.T) {
	for _, test := range []struct {
		name          string
		server        int
		dump, restore string
		valid         bool
	}{
		{"pg15", 150017, "pg_dump (PostgreSQL) 15.17 (Debian)", "pg_restore (PostgreSQL) 15.16", true},
		{"pg16", 160010, "pg_dump (PostgreSQL) 16.10", "pg_restore (PostgreSQL) 16.10", true},
		{"unsupported_old", 140020, "pg_dump (PostgreSQL) 14.20", "pg_restore (PostgreSQL) 14.20", false},
		{"unsupported_new", 170000, "pg_dump (PostgreSQL) 17.0", "pg_restore (PostgreSQL) 17.0", false},
		{"new_dump", 150017, "pg_dump (PostgreSQL) 16.10", "pg_restore (PostgreSQL) 15.17", false},
		{"old_restore", 160010, "pg_dump (PostgreSQL) 16.10", "pg_restore (PostgreSQL) 15.17", false},
		{"swapped", 150017, "pg_restore (PostgreSQL) 15.17", "pg_dump (PostgreSQL) 15.17", false},
		{"malformed", 150017, "pg_dump (PostgreSQL) 15oops", "pg_restore (PostgreSQL) 15.17", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := checkToolVersions(test.server, test.dump, test.restore)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}
