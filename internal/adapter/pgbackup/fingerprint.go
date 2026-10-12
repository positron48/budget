package pgbackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

type TableProof struct {
	Schema, Name, SHA256 string
	Rows                 int64
}
type Migration struct {
	Version int64
	Dirty   bool
}
type DatabaseSettings struct{ Encoding, Collation, CType, LocaleProvider, ICULocale string }
type State struct {
	SchemaSHA256  string
	Tables        []TableProof
	Migration     Migration
	Database      DatabaseSettings
	ServerVersion int
}

func snapshotTx(ctx context.Context, conn *pgx.Conn) (pgx.Tx, string, error) {
	tx, e := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, "", errors.New("cannot begin database snapshot")
	}
	if _, e = tx.Exec(ctx, `SET LOCAL row_security=off; SET LOCAL timezone='UTC'; SET LOCAL datestyle='ISO, YMD'; SET LOCAL extra_float_digits=3; SET LOCAL intervalstyle='postgres'; SET LOCAL search_path=pg_catalog,public`); e != nil {
		_ = tx.Rollback(ctx)
		return nil, "", errors.New("cannot configure database snapshot")
	}
	var id string
	if e = tx.QueryRow(ctx, `SELECT pg_export_snapshot()`).Scan(&id); e != nil {
		_ = tx.Rollback(ctx)
		return nil, "", errors.New("cannot export database snapshot")
	}
	return tx, id, nil
}

// Version comments and pg_dump's random psql restriction token are transport
// metadata; ownership is explicitly remapped on restore. SQL definitions,
// function bodies, object comments and object order remain checked.
var objectOwnerComment = regexp.MustCompile(`^(-- Name: .*; Type: .*; Schema: .*; Owner:) .*$`)

// With a legacy public owner, pg_dump emits this extra archive-header comment
// even though it explicitly does not create the schema. A fresh PostgreSQL 15
// target owns public via pg_database_owner and omits it. It contains no SQL;
// actual CREATE/COMMENT/function statements must remain byte-for-byte checked.
var implicitPublicSchemaComment = regexp.MustCompile(`\n--\n-- Name: public; Type: SCHEMA; Schema: -; Owner: <restore-owner>\n--\n\n-- \*not\* creating schema, since initdb creates it\n\n`)

func normalizeSchema(raw []byte) []byte {
	lines := strings.Split(string(raw), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "-- Dumped from database version ") || strings.HasPrefix(line, "-- Dumped by pg_dump version ") || strings.HasPrefix(line, "\\restrict ") || strings.HasPrefix(line, "\\unrestrict ") {
			continue
		}
		out = append(out, objectOwnerComment.ReplaceAllString(line, "$1 <restore-owner>"))
	}
	return []byte(strings.TrimSpace(implicitPublicSchemaComment.ReplaceAllString(strings.Join(out, "\n"), "")))
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 32<<20 {
		return 0, errors.New("database schema exceeds verification limit")
	}
	return b.Buffer.Write(p)
}
func inspect(ctx context.Context, tx pgx.Tx, tools Tools, dsn, snapshot string, legacyExpected string) (State, error) {
	var state State
	var unsupported bool
	if e := tx.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&state.ServerVersion); e != nil {
		return state, errors.New("server version unavailable")
	}
	if e := tx.QueryRow(ctx, `SELECT pg_encoding_to_char(encoding),datcollate,datctype,datlocprovider::text,COALESCE(daticulocale,'') FROM pg_database WHERE datname=current_database()`).Scan(&state.Database.Encoding, &state.Database.Collation, &state.Database.CType, &state.Database.LocaleProvider, &state.Database.ICULocale); e != nil {
		return state, errors.New("database locale unavailable; PostgreSQL 15 or 16 is required")
	}
	if e := tx.QueryRow(ctx, `SELECT version,dirty FROM public.schema_migrations`).Scan(&state.Migration.Version, &state.Migration.Dirty); e != nil || state.Migration.Dirty {
		return state, errors.New("a clean Budget schema_migrations row is required")
	}
	// Budget has neither foreign tables nor large objects. Refuse to certify a
	// future schema we cannot fully fingerprint rather than silently omit it.
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_largeobject_metadata) OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema' AND c.relkind='f')`).Scan(&unsupported); e != nil || unsupported {
		return state, errors.New("large objects or foreign tables require an extended verifier")
	}
	rows, e := tx.Query(ctx, `SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema' AND c.relkind IN ('r','p','m') ORDER BY n.nspname COLLATE "C",c.relname COLLATE "C"`)
	if e != nil {
		return state, errors.New("cannot inventory database tables")
	}
	tables := []TableProof{}
	for rows.Next() {
		var t TableProof
		if e = rows.Scan(&t.Schema, &t.Name); e != nil {
			break
		}
		tables = append(tables, t)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return state, errors.New("cannot inventory database tables")
	}
	for _, t := range tables {
		id := pgx.Identifier{t.Schema, t.Name}.Sanitize()
		rows, e = tx.Query(ctx, `SELECT to_jsonb(t)::text FROM ONLY `+id+` t ORDER BY to_jsonb(t)::text COLLATE "C"`)
		if e != nil {
			return state, errors.New("cannot fingerprint database table")
		}
		sum := sha256.New()
		for rows.Next() {
			var value string
			if e = rows.Scan(&value); e != nil {
				break
			}
			var length [8]byte
			binary.BigEndian.PutUint64(length[:], uint64(len(value)))
			sum.Write(length[:])
			sum.Write([]byte(value))
			t.Rows++
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return state, errors.New("cannot fingerprint database rows")
		}
		t.SHA256 = hex.EncodeToString(sum.Sum(nil))
		state.Tables = append(state.Tables, t)
	}
	if e = verifySequences(ctx, tx); e != nil {
		return state, e
	}
	schema := &boundedBuffer{}
	if e = tools.Dump(ctx, dsn, snapshot, true, schema); e != nil {
		return state, e
	}
	normalized := normalizeSchema(schema.Bytes())
	sum := sha256.Sum256(normalized)
	state.SchemaSHA256 = hex.EncodeToString(sum[:])
	// Package 157 included the implicit public archive header in its hash.
	// Accept that exact historical representation, without changing the manifest
	// or discarding any SQL. All other state still uses the ordinary comparison.
	if legacyExpected != "" && state.SchemaSHA256 != legacyExpected {
		legacy := legacyPublicSchema(normalized)
		legacySum := sha256.Sum256(legacy)
		if hex.EncodeToString(legacySum[:]) == legacyExpected {
			state.SchemaSHA256 = legacyExpected
		}
	}
	return state, nil
}

func legacyPublicSchema(normalized []byte) []byte {
	const anchor = "SET row_security = off;\n\n"
	const header = "--\n-- Name: public; Type: SCHEMA; Schema: -; Owner: <restore-owner>\n--\n\n-- *not* creating schema, since initdb creates it\n\n\n"
	return []byte(strings.Replace(string(normalized), anchor, anchor+header, 1))
}
func verifySequences(ctx context.Context, tx pgx.Tx) error {
	rows, e := tx.Query(ctx, `SELECT sn.nspname,s.relname,tn.nspname,t.relname,a.attname,q.seqincrement::text,q.seqmax::text,q.seqcycle FROM pg_class s JOIN pg_namespace sn ON sn.oid=s.relnamespace JOIN pg_sequence q ON q.seqrelid=s.oid JOIN pg_depend d ON d.classid='pg_class'::regclass AND d.objid=s.oid AND d.deptype IN ('a','i') JOIN pg_class t ON t.oid=d.refobjid JOIN pg_namespace tn ON tn.oid=t.relnamespace JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=d.refobjsubid WHERE s.relkind='S' AND sn.nspname !~ '^pg_'`)
	if e != nil {
		return errors.New("cannot inventory identity sequences")
	}
	type item struct {
		s, n, ts, tn, col, step, limit string
		cycle                          bool
	}
	items := []item{}
	for rows.Next() {
		var i item
		if e = rows.Scan(&i.s, &i.n, &i.ts, &i.tn, &i.col, &i.step, &i.limit, &i.cycle); e != nil {
			break
		}
		items = append(items, i)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return errors.New("cannot inventory identity sequences")
	}
	for _, i := range items {
		step, ok := new(big.Int).SetString(i.step, 10)
		if !ok || step.Sign() <= 0 || i.cycle {
			return errors.New("unsupported identity sequence policy")
		}
		var last, maximum string
		var called bool
		if e = tx.QueryRow(ctx, `SELECT last_value::text,is_called FROM `+pgx.Identifier{i.s, i.n}.Sanitize()).Scan(&last, &called); e != nil {
			return errors.New("cannot read identity sequence")
		}
		if e = tx.QueryRow(ctx, `SELECT COALESCE(max(`+pgx.Identifier{i.col}.Sanitize()+`)::text,'') FROM `+pgx.Identifier{i.ts, i.tn}.Sanitize()).Scan(&maximum); e != nil {
			return errors.New("cannot check identity sequence")
		}
		next, ok := new(big.Int).SetString(last, 10)
		limit, lok := new(big.Int).SetString(i.limit, 10)
		if !ok || !lok {
			return errors.New("invalid identity sequence")
		}
		if called {
			next.Add(next, step)
		}
		if next.Cmp(limit) > 0 {
			return errors.New("identity sequence is exhausted")
		}
		if maximum == "" {
			continue
		}
		max, mok := new(big.Int).SetString(maximum, 10)
		if !mok {
			return errors.New("invalid identity sequence")
		}

		if next.Cmp(max) <= 0 {
			return fmt.Errorf("identity sequence does not follow restored rows: %s.%s", i.s, i.n)
		}
	}
	return nil
}
