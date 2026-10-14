package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
)

// CheckInvestmentArchiveRestore executes the same restoration as apply, but
// always rolls it back, including the receipt, sequence restart and NAV jobs.
func (r *InvestmentRepo) CheckInvestmentArchiveRestore(ctx context.Context, actor string, bundle archive.Bundle) (archive.JournalVerification, error) {
	return r.restoreInvestmentArchive(ctx, actor, bundle, false)
}

// RestoreInvestmentArchive restores into an empty matching tenant atomically.
// The expected checksum binds an operator's reviewed file to the apply action.
// A retry of a committed source hash returns its receipt without changing data.
func (r *InvestmentRepo) RestoreInvestmentArchive(ctx context.Context, actor string, bundle archive.Bundle, expectedSHA string) (archive.JournalVerification, error) {
	if expectedSHA == "" || expectedSHA != bundle.SHA256 {
		return archive.JournalVerification{}, d.ErrInvestmentPrecondition
	}
	return r.restoreInvestmentArchive(ctx, actor, bundle, true)
}
func (r *InvestmentRepo) restoreInvestmentArchive(ctx context.Context, actor string, bundle archive.Bundle, apply bool) (archive.JournalVerification, error) {
	return r.restoreInvestmentArchiveWithReplacement(ctx, actor, bundle, apply, nil)
}
func (r *InvestmentRepo) restoreInvestmentArchiveWithReplacement(ctx context.Context, actor string, bundle archive.Bundle, apply bool, replacement *PortfolioArchiveReplacement) (archive.JournalVerification, error) {
	var empty archive.JournalVerification
	sealed, err := archive.Seal(bundle.Data)
	if err != nil || bundle.Format != archive.Format || sealed.SHA256 != bundle.SHA256 {
		return empty, d.ErrInvestmentPrecondition
	}
	verified, err := archive.VerifyJournal(bundle.Data)
	if err != nil {
		return empty, err
	}
	tx, err := r.pool.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return empty, err
	}
	// Use a fresh context so cancellation cannot prevent rollback.
	defer func() { _ = tx.Rollback(context.Background()) }()
	tenant := bundle.Data.TenantID
	// Match broker-link lock order and exclude concurrent snapshot uploads while
	// checking emptiness and restoring immutable observations and claims.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "broker-import:"+tenant); err != nil {
		return empty, err
	}
	if err = lockInvestment(ctx, tx, tenant); err != nil {
		return empty, err
	}
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, tenant, actor).Scan(&role)
	if err == pgx.ErrNoRows || err == nil && role != "owner" && role != "admin" {
		return empty, d.ErrInvestmentForbidden
	}
	if err != nil {
		return empty, err
	}
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&locked); err != nil {
		return empty, err
	}
	if replacement != nil && replacement.Market != nil {
		if err = lockPortfolioMarket(ctx, tx, tenant, *replacement.Market); err != nil {
			return empty, err
		}
	}
	if replacement != nil {
		if err = lockPortfolioArchiveTables(ctx, tx); err != nil {
			return empty, err
		}
	}
	if replacement != nil && apply {
		var payload string
		var raw []byte
		e := tx.QueryRow(ctx, `SELECT payload_hash,result FROM financial_write_requests WHERE tenant_id=$1 AND operation='investment.archive.replace' AND request_key=$2`, tenant, bundle.SHA256).Scan(&payload, &raw)
		if e == nil {
			if payload != investmentHash([]string{actor, replacement.ExpectedStateSHA256, bundle.SHA256, replacement.ExpectedMarketSHA256}) || json.Unmarshal(raw, &empty) != nil {
				return empty, d.ErrInvestmentConflict
			}
			replacement.AlreadyApplied = true
			return empty, tx.Commit(ctx)
		}
		if e != pgx.ErrNoRows {
			return empty, e
		}
	}
	if apply && replacement == nil {
		var raw []byte
		err = tx.QueryRow(ctx, `SELECT verification FROM investment_archive_restores WHERE tenant_id=$1 AND source_sha256=$2`, tenant, bundle.SHA256).Scan(&raw)
		if err == nil {
			if json.Unmarshal(raw, &empty) != nil {
				return empty, d.ErrInvestmentPrecondition
			}
			return empty, tx.Commit(ctx)
		}
		if err != pgx.ErrNoRows {
			return empty, err
		}
	}
	// Serialize explicit serial IDs with ordinary mapping writes across tenants.
	// The lock also protects the transactional sequence restart below.
	if _, err = tx.Exec(ctx, `LOCK TABLE investment_import_mapping_revisions IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return empty, err
	}
	if replacement != nil {
		before, e := readInvestmentArchive(ctx, tx, tenant, actor)
		if e != nil {
			return empty, e
		}
		hash, e := InvestmentArchiveStateSHA256(before)
		if e != nil {
			return empty, e
		}
		if hash != replacement.ExpectedStateSHA256 {
			return empty, d.ErrInvestmentConflict
		}
		if !samePortfolioAccounts(before.Data, bundle.Data) {
			return empty, d.ErrInvestmentPrecondition
		}
		if e = clearPortfolioArchiveTx(ctx, tx, tenant); e != nil {
			return empty, e
		}
	}
	for name := range archive.Excluded {
		if name == "investment_valuation_generations" || name == "investment_nav_invalidations" {
			continue
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+pgx.Identifier{"public", name}.Sanitize()+` WHERE tenant_id=$1)`, tenant).Scan(&exists); err != nil {
			return empty, err
		}
		if exists {
			return empty, d.ErrInvestmentPrecondition
		}
	}
	target, err := readInvestmentArchive(ctx, tx, tenant, actor)
	if err != nil {
		return empty, err
	}
	tables := map[string]archive.Table{}
	for _, table := range bundle.Data.Tables {
		tables[table.Name] = table
	}
	for _, table := range target.Data.Tables {
		if len(table.Rows) != 0 || !reflect.DeepEqual(table.Columns, tables[table.Name].Columns) {
			return empty, d.ErrInvestmentPrecondition
		}
	}
	for _, id := range bundle.Data.Actors {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, id).Scan(&exists); err != nil {
			return empty, err
		}
		if !exists {
			return empty, d.ErrInvestmentPrecondition
		}
	}
	for _, name := range archive.Included {
		table := tables[name]
		if len(table.Rows) == 0 {
			continue
		}
		columns := make([]string, len(table.Columns))
		for i, column := range table.Columns {
			columns[i] = pgx.Identifier{column.Name}.Sanitize()
		}
		names := strings.Join(columns, ",")
		identifier := pgx.Identifier{"public", name}.Sanitize()
		raw, err := json.Marshal(table.Rows)
		if err != nil {
			return empty, err
		}
		// Explicit columns preserve serial values without invoking nextval (which
		// would survive rollback). Identifiers have been matched to the live schema.
		_, err = tx.Exec(ctx, `INSERT INTO `+identifier+` (`+names+`) SELECT `+names+` FROM jsonb_populate_recordset(NULL::`+identifier+`, $1::jsonb)`, raw)
		if err != nil {
			return empty, d.ErrInvestmentPrecondition
		}
	}
	if _, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		return empty, d.ErrInvestmentPrecondition
	}
	if err = verifyArchiveGroups(ctx, tx, tenant); err != nil {
		return empty, err
	}
	if err = verifyBrokerArchiveSources(ctx, tx, tenant); err != nil {
		return empty, err
	}
	if err = verifyBrokerArchiveLinks(ctx, tx, tenant); err != nil {
		return empty, err
	}
	restored, err := readInvestmentArchive(ctx, tx, tenant, actor)
	if err != nil {
		return empty, err
	}
	// PostgreSQL must preserve every exported field, including exact numerics,
	// source bytes and audit rows. Row order is irrelevant; representation uses
	// the same to_jsonb exporter on both sides, normalized for file encoding.
	expected := bundle.Data
	expected.Tables = append([]archive.Table(nil), bundle.Data.Tables...)
	for i := range expected.Tables {
		expected.Tables[i].Rows = append([]json.RawMessage(nil), expected.Tables[i].Rows...)
	}
	expected.Actors = append([]string(nil), expected.Actors...)
	archive.Canonicalize(&expected)
	for i, table := range restored.Data.Tables {
		other := expected.Tables[i]
		if table.Name != other.Name || len(table.Rows) != len(other.Rows) {
			return empty, d.ErrInvestmentPrecondition
		}
		equal, e := archiveRowsEqual(table.Rows, other.Rows)
		if e != nil || !equal {
			return empty, d.ErrInvestmentPrecondition
		}
	}
	result, err := archive.VerifyJournal(restored.Data)
	if err != nil || result != verified {
		return empty, d.ErrInvestmentPrecondition
	}
	if replacement != nil && replacement.Market != nil {
		if err = publishPortfolioMarketTx(ctx, tx, tenant, *replacement.Market); err != nil {
			return empty, err
		}
	}
	if err = restartArchiveSequences(ctx, tx); err != nil {
		return empty, err
	}
	if err = restoreArchiveProjections(ctx, tx, tenant); err != nil {
		return empty, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO investment_archive_restores(tenant_id,source_sha256,actor_id,source_created_at,verification) VALUES($1,$2,$3,$4,$5)`, tenant, bundle.SHA256, actor, bundle.Data.CreatedAt, raw); err != nil {
		return empty, err
	}
	if replacement != nil {
		if _, err = tx.Exec(ctx, `INSERT INTO financial_write_requests(tenant_id,operation,request_key,payload_hash,result) VALUES($1,'investment.archive.replace',$2,$3,$4)`, tenant, bundle.SHA256, investmentHash([]string{actor, replacement.ExpectedStateSHA256, bundle.SHA256, replacement.ExpectedMarketSHA256}), raw); err != nil {
			return empty, err
		}
	}
	if apply {
		return result, tx.Commit(ctx)
	}
	if err = tx.Rollback(ctx); err != nil {
		return empty, err
	}
	return result, nil
}

// JSON encoding compacts RawMessages and escapes HTML; those representation
// changes are not lost database values. UseNumber retains every numeric digit.
// Sorting canonical values also prevents whitespace from changing row order.
func archiveRowsEqual(left, right []json.RawMessage) (bool, error) {
	normalize := func(rows []json.RawMessage) ([]string, error) {
		out := make([]string, len(rows))
		for i, row := range rows {
			decoder := json.NewDecoder(bytes.NewReader(row))
			decoder.UseNumber()
			var value any
			if e := decoder.Decode(&value); e != nil {
				return nil, e
			}
			raw, e := json.Marshal(value)
			if e != nil {
				return nil, e
			}
			out[i] = string(raw)
		}
		sort.Strings(out)
		return out, nil
	}
	a, e := normalize(left)
	if e != nil {
		return false, e
	}
	b, e := normalize(right)
	if e != nil {
		return false, e
	}
	return reflect.DeepEqual(a, b), nil
}
