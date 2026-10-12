package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
)

// ExportInvestmentArchive is an owner/admin whole-tenant export. All tables and
// source bytes are read from one snapshot; provider cache and auth are excluded.
func (r *InvestmentRepo) ExportInvestmentArchive(ctx context.Context, tenant, actor string) (archive.Bundle, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return archive.Bundle{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, e := readInvestmentArchive(ctx, tx, tenant, actor)
	if e != nil {
		return result, e
	}
	return result, tx.Commit(ctx)
}
func readInvestmentArchive(ctx context.Context, tx pgx.Tx, tenant, actor string) (archive.Bundle, error) {
	var role string
	e := tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, tenant, actor).Scan(&role)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && role != "owner" && role != "admin" {
		return archive.Bundle{}, d.ErrInvestmentForbidden
	}
	if e != nil {
		return archive.Bundle{}, e
	}
	data := archive.Dataset{TenantID: tenant, Tables: []archive.Table{}, Actors: []string{}, Excluded: map[string]string{}}
	if e = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&data.CreatedAt); e != nil {
		return archive.Bundle{}, e
	}
	classified := map[string]bool{}
	for _, name := range archive.Included {
		classified[name] = true
	}
	for name, reason := range archive.Excluded {
		classified[name] = true
		data.Excluded[name] = reason
	}
	rows, e := tx.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND starts_with(tablename,'investment_')`)
	if e != nil {
		return archive.Bundle{}, e
	}
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			rows.Close()
			return archive.Bundle{}, e
		}
		if !classified[name] {
			rows.Close()
			return archive.Bundle{}, d.ErrInvestmentPrecondition
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return archive.Bundle{}, e
	}
	actors := map[string]bool{}
	total, size := 0, 0
	for _, name := range archive.Included {
		table := archive.Table{Name: name, Columns: []archive.Column{}, Rows: []json.RawMessage{}}
		columns, e := tx.Query(ctx, `SELECT attname,format_type(atttypid,atttypmod),attnotnull FROM pg_attribute WHERE attrelid=to_regclass($1) AND attnum>0 AND NOT attisdropped ORDER BY attnum`, "public."+name)
		if e != nil {
			return archive.Bundle{}, e
		}
		for columns.Next() {
			var column archive.Column
			if e = columns.Scan(&column.Name, &column.Type, &column.NotNull); e != nil {
				columns.Close()
				return archive.Bundle{}, e
			}
			table.Columns = append(table.Columns, column)
		}
		e = columns.Err()
		columns.Close()
		if e != nil {
			return archive.Bundle{}, e
		}
		// Identifier originates only from the fixed package manifest, never a client.
		filter := ""
		if name == "financial_write_requests" {
			filter = " AND starts_with(operation,'investment.')"
		}
		rows, e = tx.Query(ctx, `SELECT to_jsonb(t) FROM `+pgx.Identifier{"public", name}.Sanitize()+` t WHERE tenant_id=$1`+filter, tenant)
		if e != nil {
			return archive.Bundle{}, e
		}
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				rows.Close()
				return archive.Bundle{}, e
			}
			total++
			size += len(raw)
			if total > archive.MaxRows || size > archive.MaxBytes {
				rows.Close()
				return archive.Bundle{}, d.ErrInvestmentLimit
			}
			var fields map[string]json.RawMessage
			if e = json.Unmarshal(raw, &fields); e != nil {
				rows.Close()
				return archive.Bundle{}, e
			}
			for _, field := range []string{"actor_id", "created_by"} {
				if v, ok := fields[field]; ok && string(v) != "null" {
					var actorID string
					if e = json.Unmarshal(v, &actorID); e != nil {
						rows.Close()
						return archive.Bundle{}, e
					}
					actors[actorID] = true
				}
			}
			table.Rows = append(table.Rows, json.RawMessage(raw))
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return archive.Bundle{}, e
		}
		data.Tables = append(data.Tables, table)
	}
	for actorID := range actors {
		data.Actors = append(data.Actors, actorID)
	}
	archive.Canonicalize(&data)
	return archive.Seal(data)
}
