package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

// SaveBrokerSnapshot validates the original bytes again at the persistence
// boundary. Identical snapshots and overlapping operation versions are reused.
// New content for an existing provider ID remains a separate observation.
func (r *InvestmentImportRepo) SaveBrokerSnapshot(ctx context.Context, tenant, actor string, raw []byte) (string, error) {
	prepared, err := imp.PrepareBrokerSnapshot(raw)
	if err != nil {
		return "", err
	}
	if _, err = uuid.Parse(tenant); err != nil {
		return "", imp.ErrInvalid
	}
	if _, err = uuid.Parse(actor); err != nil {
		return "", imp.ErrInvalid
	}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "broker-import:"+tenant); err != nil {
		return "", err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM investment_broker_snapshots WHERE tenant_id=$1 AND source_sha256=$2`, tenant, prepared.SHA256).Scan(&id)
	if err == nil {
		return id, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	id = uuid.NewString()
	if _, err = tx.Exec(ctx, `INSERT INTO investment_broker_snapshots(id,tenant_id,source_sha256,account_id,original_json,row_count,created_by) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, tenant, prepared.SHA256, prepared.AccountID, prepared.Original, len(prepared.Rows), actor); err != nil {
		return "", err
	}
	links := make([][]any, 0, len(prepared.Rows))
	for i, row := range prepared.Rows {
		var observation string
		err = tx.QueryRow(ctx, `SELECT id FROM investment_broker_observations WHERE tenant_id=$1 AND account_id=$2 AND operation_id=$3 AND content_sha256=$4`, tenant, row.AccountID, row.OperationID, row.SHA256).Scan(&observation)
		if errors.Is(err, pgx.ErrNoRows) {
			observation = uuid.NewString()
			_, err = tx.Exec(ctx, `INSERT INTO investment_broker_observations(id,tenant_id,account_id,operation_id,content_sha256,raw_json) VALUES($1,$2,$3,$4,$5,$6)`, observation, tenant, row.AccountID, row.OperationID, row.SHA256, []byte(row.Raw))
		}
		if err != nil {
			return "", err
		}
		links = append(links, []any{tenant, id, i + 1, observation})
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"investment_broker_snapshot_rows"}, []string{"tenant_id", "snapshot_id", "ordinal", "observation_id"}, pgx.CopyFromRows(links)); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func (r *InvestmentImportRepo) BrokerSnapshotBytes(ctx context.Context, tenant, id string) ([]byte, error) {
	var raw []byte
	err := r.pool.DB.QueryRow(ctx, `SELECT original_json FROM investment_broker_snapshots WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&raw)
	return raw, importError(err)
}

func (r *InvestmentImportRepo) ListBrokerSnapshots(ctx context.Context, tenant string, limit, offset int) ([]imp.BrokerSnapshotInfo, error) {
	if limit < 1 || limit > 200 || offset < 0 || offset > 100000 {
		return nil, imp.ErrInvalid
	}
	rows, err := r.pool.DB.Query(ctx, `SELECT id,account_id,source_sha256,row_count,created_at FROM investment_broker_snapshots WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []imp.BrokerSnapshotInfo{}
	for rows.Next() {
		var item imp.BrokerSnapshotInfo
		if err = rows.Scan(&item.ID, &item.AccountID, &item.SHA256, &item.Rows, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *InvestmentImportRepo) BrokerSnapshotRows(ctx context.Context, tenant, id string, limit, offset int) ([]imp.BrokerStoredRow, error) {
	if limit < 1 || limit > 200 || offset < 0 || offset > 100000 {
		return nil, imp.ErrInvalid
	}
	var exists bool
	if err := r.pool.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_broker_snapshots WHERE tenant_id=$1 AND id=$2)`, tenant, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, imp.ErrNotFound
	}
	rows, err := r.pool.DB.Query(ctx, `SELECT l.ordinal,o.id,o.account_id,o.operation_id,o.content_sha256,o.raw_json,
 EXISTS(SELECT 1 FROM investment_broker_observations other WHERE other.tenant_id=o.tenant_id AND other.account_id=o.account_id AND other.operation_id=o.operation_id AND other.content_sha256<>o.content_sha256),
 COALESCE(b.id::text,''),COALESCE(b.ledger_id::text,''),COALESCE(b.operation_id::text,''),COALESCE(j.status,''),COALESCE(b.operation_version,0),COALESCE(j.version,0),COALESCE(c.content_sha256<>o.content_sha256,false)
 FROM investment_broker_snapshot_rows l JOIN investment_broker_observations o ON o.tenant_id=l.tenant_id AND o.id=l.observation_id
 LEFT JOIN investment_broker_claims c ON c.tenant_id=o.tenant_id AND c.account_id=o.account_id AND c.operation_id=o.operation_id
 LEFT JOIN investment_broker_links b ON b.tenant_id=c.tenant_id AND b.id=c.link_id
 LEFT JOIN investment_operations j ON j.tenant_id=b.tenant_id AND j.ledger_id=b.ledger_id AND j.id=b.operation_id
 WHERE l.tenant_id=$1 AND l.snapshot_id=$2 ORDER BY l.ordinal LIMIT $3 OFFSET $4`, tenant, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []imp.BrokerStoredRow{}
	for rows.Next() {
		var item imp.BrokerStoredRow
		if err = rows.Scan(&item.Ordinal, &item.ObservationID, &item.AccountID, &item.OperationID, &item.SHA256, &item.Raw, &item.HasOtherVersions, &item.LinkID, &item.LinkedLedgerID, &item.LinkedOperationID, &item.LinkedStatus, &item.LinkedVersion, &item.CurrentVersion, &item.ClaimedDifferentContent); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
