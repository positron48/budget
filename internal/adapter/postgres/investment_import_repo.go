package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

type InvestmentImportRepo struct{ pool *Pool }

func NewInvestmentImportRepo(pool *Pool) *InvestmentImportRepo { return &InvestmentImportRepo{pool} }
func importError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return imp.ErrNotFound
	}
	return err
}
func (r *InvestmentImportRepo) SaveImport(ctx context.Context, tenant, user string, p imp.Prepared) (imp.Import, error) {
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return imp.Import{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serializes source/key checks for this tenant, including concurrent retries.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "investment-import:"+tenant); err != nil {
		return imp.Import{}, err
	}
	var savedHash, id string
	err = tx.QueryRow(ctx, `SELECT payload_hash,result->>'id' FROM financial_write_requests WHERE tenant_id=$1 AND operation='investment.import.upload' AND request_key=$2`, tenant, p.Input.RequestKey).Scan(&savedHash, &id)
	if err == nil {
		if savedHash != p.PayloadHash {
			return imp.Import{}, imp.ErrConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return imp.Import{}, err
		}
		return r.GetImport(ctx, tenant, id)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return imp.Import{}, err
	}
	err = tx.QueryRow(ctx, `SELECT id,initial_payload_hash FROM investment_imports WHERE tenant_id=$1 AND source_sha256=$2`, tenant, p.Source.Audit.SHA256).Scan(&id, &savedHash)
	switch {
	case err == nil:
		if savedHash != p.PayloadHash {
			return imp.Import{}, imp.ErrConflict
		}
	case errors.Is(err, pgx.ErrNoRows):
		id = uuid.NewString()
		_, err = tx.Exec(ctx, `INSERT INTO investment_imports(id,tenant_id,source_sha256,filename,original_csv,preview_hash,initial_payload_hash,row_count,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, tenant, p.Source.Audit.SHA256, p.Input.Filename, p.Input.CSV, p.Preview.Hash, p.PayloadHash, len(p.Source.Rows), user)
		if err != nil {
			return imp.Import{}, err
		}
		accounts := map[string]string{}
		for _, a := range p.Input.Accounts {
			aid := uuid.NewString()
			accounts[a.SourceKey] = aid
			if _, err = tx.Exec(ctx, `INSERT INTO investment_import_accounts(id,tenant_id,import_id,source_key,name) VALUES($1,$2,$3,$4,$5)`, aid, tenant, id, a.SourceKey, a.Name); err != nil {
				return imp.Import{}, err
			}
		}
		assignments := map[int]imp.Assignment{}
		for _, a := range p.Input.Assignments {
			assignments[a.Record] = a
		}
		records := make([][]any, 0, len(p.Source.Rows))
		for i, row := range p.Source.Rows {
			raw, _ := json.Marshal(row)
			preview, _ := json.Marshal(p.Preview.Rows[i])
			a := assignments[row.Record]
			initialMapping, _ := json.Marshal(a)
			records = append(records, []any{tenant, id, row.Record, row.ID, accounts[a.AccountKey], raw, preview, a.Evidence, initialMapping})
		}
		_, err = tx.CopyFrom(ctx, pgx.Identifier{"investment_import_rows"}, []string{"tenant_id", "import_id", "record", "source_row_id", "account_id", "source", "preview", "mapping_evidence", "initial_mapping"}, pgx.CopyFromRows(records))
		if err != nil {
			return imp.Import{}, err
		}
	default:
		return imp.Import{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO financial_write_requests(tenant_id,operation,request_key,payload_hash,result) VALUES($1,'investment.import.upload',$2,$3,jsonb_build_object('id',$4::text))`, tenant, p.Input.RequestKey, p.PayloadHash, id)
	if err != nil {
		return imp.Import{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return imp.Import{}, err
	}
	return r.GetImport(ctx, tenant, id)
}

const importColumns = `id,filename,source_sha256,state,row_count,version,created_at,preview_hash`

func scanImport(row pgx.Row) (imp.Import, error) {
	var v imp.Import
	err := row.Scan(&v.ID, &v.Filename, &v.SourceSHA256, &v.State, &v.Rows, &v.Version, &v.CreatedAt, &v.PreviewHash)
	return v, importError(err)
}
func (r *InvestmentImportRepo) GetImport(ctx context.Context, tenant, id string) (imp.Import, error) {
	// Repeatable read keeps account counts consistent with the import version.
	tx, err := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return imp.Import{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	v, err := scanImport(tx.QueryRow(ctx, `SELECT `+importColumns+` FROM investment_imports WHERE tenant_id=$1 AND id=$2`, tenant, id))
	if err != nil {
		return v, err
	}
	rows, err := tx.Query(ctx, `SELECT a.id,a.source_key,a.name,count(r.record) FROM investment_import_accounts a LEFT JOIN investment_import_rows r ON (r.tenant_id,r.import_id,r.account_id)=(a.tenant_id,a.import_id,a.id) WHERE a.tenant_id=$1 AND a.import_id=$2 GROUP BY a.id ORDER BY a.source_key`, tenant, id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	v.Accounts = []imp.ImportAccount{}
	for rows.Next() {
		var a imp.ImportAccount
		if err = rows.Scan(&a.ID, &a.SourceKey, &a.Name, &a.Rows); err != nil {
			return v, err
		}
		v.Accounts = append(v.Accounts, a)
	}
	if err = rows.Err(); err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
func (r *InvestmentImportRepo) ListImports(ctx context.Context, tenant string, limit, offset int) ([]imp.Import, error) {
	rows, err := r.pool.DB.Query(ctx, `SELECT `+importColumns+` FROM investment_imports WHERE tenant_id=$1 ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []imp.Import{}
	for rows.Next() {
		v, err := scanImport(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

const importRowColumns = `record,source_row_id,account_id,source,preview,mapping_evidence,version`

func scanImportRow(row pgx.Row, extra ...any) (imp.StoredRow, error) {
	var v imp.StoredRow
	var raw, preview []byte
	dest := append([]any{&v.Record, &v.SourceRowID, &v.AccountID, &raw, &preview, &v.MappingEvidence, &v.Version}, extra...)
	if err := row.Scan(dest...); err != nil {
		return v, importError(err)
	}
	if err := json.Unmarshal(raw, &v.Source); err != nil {
		return v, err
	}
	err := json.Unmarshal(preview, &v.Preview)
	return v, err
}
func (r *InvestmentImportRepo) ListImportRows(ctx context.Context, tenant, id string, limit, offset int) ([]imp.StoredRow, error) {
	var exists bool
	if err := r.pool.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_imports WHERE tenant_id=$1 AND id=$2)`, tenant, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, imp.ErrNotFound
	}
	rows, err := r.pool.DB.Query(ctx, `SELECT `+importRowColumns+`, (SELECT jsonb_build_object('LedgerID',l.ledger_id,'OperationID',l.operation_id,'OperationStatus',o.status) FROM investment_source_links l JOIN investment_operations o ON o.tenant_id=l.tenant_id AND o.ledger_id=l.ledger_id AND o.id=l.operation_id WHERE l.tenant_id=investment_import_rows.tenant_id AND l.import_id=investment_import_rows.import_id AND l.record=investment_import_rows.record) FROM investment_import_rows WHERE tenant_id=$1 AND import_id=$2 ORDER BY record LIMIT $3 OFFSET $4`, tenant, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []imp.StoredRow{}
	for rows.Next() {
		var link []byte
		v, err := scanImportRow(rows, &link)
		if err != nil {
			return nil, err
		}
		if len(link) > 0 {
			if err = json.Unmarshal(link, &v); err != nil {
				return nil, err
			}
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (r *InvestmentImportRepo) ReassignImportRow(ctx context.Context, tenant, user, id string, record int, account string, version int64, reason string) (imp.StoredRow, error) {
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return imp.StoredRow{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Lock import before row: every remapping uses the same lock order.
	var state string
	if err = tx.QueryRow(ctx, `SELECT state FROM investment_imports WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&state); err != nil {
		return imp.StoredRow{}, importError(err)
	}
	var target string
	if err = tx.QueryRow(ctx, `SELECT id FROM investment_import_accounts WHERE tenant_id=$1 AND import_id=$2 AND id=$3`, tenant, id, account).Scan(&target); err != nil {
		return imp.StoredRow{}, importError(err)
	}
	old, err := scanImportRow(tx.QueryRow(ctx, `SELECT `+importRowColumns+` FROM investment_import_rows WHERE tenant_id=$1 AND import_id=$2 AND record=$3 FOR UPDATE`, tenant, id, record))
	if err != nil {
		return old, err
	}
	if old.Version != version {
		return old, imp.ErrConflict
	}
	if old.AccountID == target {
		return old, tx.Commit(ctx)
	}
	var posted bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_source_links WHERE tenant_id=$1 AND import_id=$2 AND record=$3)`, tenant, id, record).Scan(&posted); err != nil {
		return old, err
	}
	if posted {
		return old, imp.ErrConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO investment_import_mapping_revisions(tenant_id,import_id,record,from_account_id,to_account_id,version,reason,actor_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tenant, id, record, old.AccountID, target, version+1, reason, user)
	if err != nil {
		return old, err
	}
	result, err := scanImportRow(tx.QueryRow(ctx, `UPDATE investment_import_rows SET account_id=$4,version=version+1,mapping_evidence='manual_reassignment' WHERE tenant_id=$1 AND import_id=$2 AND record=$3 RETURNING `+importRowColumns, tenant, id, record, target))
	if err != nil {
		return old, err
	}
	if _, err = tx.Exec(ctx, `UPDATE investment_imports SET version=version+1 WHERE tenant_id=$1 AND id=$2`, tenant, id); err != nil {
		return old, err
	}
	return result, tx.Commit(ctx)
}
