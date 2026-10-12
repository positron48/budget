package pgbackup

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
)

// UpgradeReport certifies an upgrade only on a newly restored, isolated database.
// Restore contains the verified pre-upgrade state; State is the final state.
type UpgradeReport struct {
	Restore         Report
	State           State
	PreservedTables int
	PreservedRows   int64
	RestartVerified bool
	CompletedAt     time.Time
}

type legacyColumn struct{ Name, Type string }
type legacyTable struct {
	Schema, Name string
	Columns      []legacyColumn
	Proof        TableProof
}

func legacyTables(ctx context.Context, tx pgx.Tx, baseline []TableProof) ([]legacyTable, error) {
	result := make([]legacyTable, 0, len(baseline))
	for _, table := range baseline {
		if table.Schema == "public" && table.Name == "schema_migrations" {
			continue
		}
		rows, err := tx.Query(ctx, `SELECT a.attname,format_type(a.atttypid,a.atttypmod) FROM pg_attribute a WHERE a.attrelid=to_regclass($1) AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`, pgx.Identifier{table.Schema, table.Name}.Sanitize())
		if err != nil {
			return nil, errors.New("cannot inventory legacy columns")
		}
		item := legacyTable{Schema: table.Schema, Name: table.Name, Proof: table}
		for rows.Next() {
			var col legacyColumn
			if err = rows.Scan(&col.Name, &col.Type); err != nil {
				break
			}
			item.Columns = append(item.Columns, col)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil || len(item.Columns) == 0 {
			return nil, errors.New("cannot inventory legacy columns")
		}
		result = append(result, item)
	}
	return result, nil
}

func verifyLegacy(ctx context.Context, tx pgx.Tx, tables []legacyTable) error {
	for _, table := range tables {
		rows, err := tx.Query(ctx, `SELECT a.attname,format_type(a.atttypid,a.atttypmod) FROM pg_attribute a WHERE a.attrelid=to_regclass($1) AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`, pgx.Identifier{table.Schema, table.Name}.Sanitize())
		if err != nil {
			return errors.New("cannot verify legacy columns")
		}
		actual := map[string]string{}
		for rows.Next() {
			var name, typ string
			if err = rows.Scan(&name, &typ); err != nil {
				break
			}
			actual[name] = typ
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return errors.New("cannot verify legacy columns")
		}
		original := map[string]bool{}
		for _, col := range table.Columns {
			if actual[col.Name] != col.Type {
				return errors.New("upgrade changed or removed legacy columns")
			}
			original[col.Name] = true
		}
		added := []string{}
		for name := range actual {
			if !original[name] {
				added = append(added, name)
			}
		}
		// Project away only newly added columns. Original JSON keys/values, duplicate
		// rows and exact numeric values must match the verified backup fingerprint.
		rows, err = tx.Query(ctx, `SELECT (to_jsonb(t)-$1::text[])::text FROM ONLY `+pgx.Identifier{table.Schema, table.Name}.Sanitize()+` t ORDER BY (to_jsonb(t)-$1::text[])::text COLLATE "C"`, added)
		if err != nil {
			return errors.New("cannot verify legacy rows")
		}
		hash := sha256.New()
		var count int64
		for rows.Next() {
			var value string
			if err = rows.Scan(&value); err != nil {
				break
			}
			var length [8]byte
			binary.BigEndian.PutUint64(length[:], uint64(len(value)))
			hash.Write(length[:])
			hash.Write([]byte(value))
			count++
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return errors.New("cannot verify legacy rows")
		}
		if count != table.Proof.Rows || hex.EncodeToString(hash.Sum(nil)) != table.Proof.SHA256 {
			return errors.New("upgrade changed legacy rows")
		}
	}
	return nil
}

// RehearseUpgrade always restores a new database before running the supplied
// application migrator. Neither a caller-selected existing target nor the
// source database is ever passed to the migrator. Failures retain the clone.
func RehearseUpgrade(ctx context.Context, tools Tools, adminDSN, dir, name, expected string, from, to int64, migrate func(context.Context, string) error) (UpgradeReport, error) {
	var report UpgradeReport
	if from <= 0 || to <= from || migrate == nil {
		return report, errors.New("invalid upgrade rehearsal versions or migrator")
	}
	manifest, _, err := Verify(dir, expected)
	if err != nil {
		return report, err
	}
	if manifest.State.Migration != (Migration{Version: from}) {
		return report, errors.New("upgrade requires the expected clean source schema")
	}
	report.Restore, err = Restore(ctx, tools, adminDSN, dir, name, expected)
	if err != nil {
		return report, err
	}
	dsn, err := targetDSN(adminDSN, name)
	if err != nil {
		return report, err
	}
	// Every connection below is to the newly created and verified clone.
	conn, err := connect(ctx, dsn)
	if err != nil {
		return report, err
	}
	defer func() { _ = conn.Close(ctx) }()
	tx, _, err := snapshotTx(ctx, conn)
	if err != nil {
		return report, err
	}
	tables, err := legacyTables(ctx, tx, report.Restore.State.Tables)
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	if err != nil {
		return report, err
	}
	for pass := 0; pass < 2; pass++ {
		if err = ctx.Err(); err != nil {
			return report, errors.New("upgrade rehearsal cancelled")
		}
		if err = migrate(ctx, dsn); err != nil {
			return report, errors.New("isolated upgrade migration failed; SQL diagnostics suppressed")
		}
		tx, snapshot, err := snapshotTx(ctx, conn)
		if err != nil {
			return report, err
		}
		state, checkErr := inspect(ctx, tx, tools, dsn, snapshot, "")
		if checkErr == nil && state.Migration != (Migration{Version: to}) {
			checkErr = errors.New("upgrade did not reach the expected clean target schema")
		}
		if checkErr == nil {
			checkErr = verifyLegacy(ctx, tx, tables)
		}
		if checkErr == nil && pass == 1 && !reflect.DeepEqual(report.State, state) {
			checkErr = errors.New("repeat startup changed the upgraded database")
		}
		if checkErr != nil {
			_ = tx.Rollback(ctx)
			return report, checkErr
		}
		if err = tx.Commit(ctx); err != nil {
			return report, errors.New("upgrade verification transaction failed")
		}
		report.State = state
	}
	for _, table := range tables {
		report.PreservedTables++
		report.PreservedRows += table.Proof.Rows
	}
	report.RestartVerified = true
	report.CompletedAt = time.Now().UTC()
	return report, nil
}

func SaveUpgradeReport(path string, report UpgradeReport) error {
	if report.CompletedAt.IsZero() || !report.RestartVerified {
		return errors.New("cannot certify an incomplete upgrade")
	}
	return writeJSON(path, report)
}
