package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

func validBatchDraftID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func authorizeBatchDraft(ctx context.Context, tx pgx.Tx, t, u string) error {
	var role string
	err := tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, t, u).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && role != "owner" && role != "admin" {
		return d.ErrInvestmentForbidden
	}
	return err
}
func investmentBatchHash(t, u string, b inv.ImportBatch) string {
	return investmentHash(struct {
		Tenant, Actor string
		Batch         inv.ImportBatch
	}{t, u, b})
}
func readBatchDraft(ctx context.Context, tx pgx.Tx, t, u, id string) (inv.BatchDraft, error) {
	var out inv.BatchDraft
	var raw, preview, cancellation []byte
	err := tx.QueryRow(ctx, `SELECT d.id,d.version,d.data,COALESCE(f.result,d.preview),d.updated_at,c.result
 FROM investment_batch_drafts d LEFT JOIN financial_write_requests f ON f.tenant_id=d.tenant_id AND f.operation='investment.import.commit' AND f.request_key=d.request_key AND f.payload_hash=d.payload_hash
 LEFT JOIN investment_batch_cancellations c ON c.tenant_id=d.tenant_id AND c.batch_request_key=d.request_key
 WHERE d.tenant_id=$1 AND d.actor_id=$2 AND d.id=$3`, t, u, id).Scan(&out.ID, &out.Version, &raw, &preview, &out.UpdatedAt, &cancellation)
	if err != nil {
		return out, investmentError(err)
	}
	if err = json.Unmarshal(raw, &out.Batch); err != nil {
		return out, err
	}
	if len(preview) > 0 {
		out.Preview = &inv.ImportBatchResult{}
		if err = json.Unmarshal(preview, out.Preview); err != nil {
			return out, err
		}
	}
	if len(cancellation) > 0 {
		out.Cancellation = &inv.BatchCancellationResult{}
		if err = json.Unmarshal(cancellation, out.Cancellation); err != nil {
			return out, err
		}
	}
	return out, nil
}
func (r *InvestmentRepo) GetBatchDraft(ctx context.Context, t, u, id string) (inv.BatchDraft, error) {
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return inv.BatchDraft{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = authorizeBatchDraft(ctx, tx, t, u); err != nil {
		return inv.BatchDraft{}, err
	}
	out, err := readBatchDraft(ctx, tx, t, u, id)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *InvestmentRepo) SaveBatchDraft(ctx context.Context, t, u string, w inv.BatchDraftWrite) (inv.BatchDraft, error) {
	var empty inv.BatchDraft
	// The repository also checks payload identities for internal callers.
	b, err := inv.NormalizeImportBatch(t, u, w.Batch)
	if err != nil {
		return empty, err
	}
	w.Batch = b
	if w.ExpectedVersion < 0 || !validBatchDraftID(w.ID) {
		return empty, d.ErrInvestmentInvalid
	}
	raw, err := json.Marshal(b)
	if err != nil || len(raw) > 20*1024*1024 {
		return empty, d.ErrInvestmentLimit
	}
	hash := investmentBatchHash(t, u, b)
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = lockInvestment(ctx, tx, t); err != nil {
		return empty, err
	}
	if err = authorizeBatchDraft(ctx, tx, t, u); err != nil {
		return empty, err
	}
	current, err := readBatchDraft(ctx, tx, t, u, w.ID)
	if err != nil && !errors.Is(err, d.ErrInvestmentNotFound) {
		return empty, err
	}
	found := err == nil
	if found && current.Version == w.ExpectedVersion+1 && investmentBatchHash(t, u, current.Batch) == hash {
		return current, tx.Commit(ctx)
	}
	if found && (current.Version != w.ExpectedVersion || current.Preview != nil && current.Preview.Committed) {
		return empty, d.ErrInvestmentConflict
	}
	if !found && w.ExpectedVersion != 0 {
		return empty, d.ErrInvestmentNotFound
	}
	if found && current.Batch.RequestKey != b.RequestKey {
		return empty, d.ErrInvestmentConflict
	}
	if found {
		var used bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM financial_write_requests WHERE tenant_id=$1 AND operation='investment.import.commit' AND request_key=$2)`, t, current.Batch.RequestKey).Scan(&used); err != nil {
			return empty, err
		}
		if used {
			return empty, d.ErrInvestmentPrecondition
		}
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_imports WHERE tenant_id=$1 AND id=$2)`, t, b.ImportID).Scan(&exists); err != nil {
		return empty, err
	}
	if !exists {
		return empty, d.ErrInvestmentNotFound
	}
	ids := make([]string, 0, len(b.ExpectedLedgerVersions))
	for id := range b.ExpectedLedgerVersions {
		ids = append(ids, id)
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM investment_ledgers WHERE tenant_id=$1 AND id=ANY($2::uuid[])`, t, ids).Scan(&count); err != nil {
		return empty, err
	}
	if count != len(ids) {
		return empty, d.ErrInvestmentNotFound
	}
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM financial_write_requests WHERE tenant_id=$1 AND operation='investment.import.commit' AND request_key=$2)`, t, b.RequestKey).Scan(&exists); err != nil {
		return empty, err
	}
	if exists {
		return empty, d.ErrInvestmentConflict
	}
	if !found {
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM investment_batch_drafts d WHERE tenant_id=$1 AND actor_id=$2 AND NOT EXISTS(SELECT 1 FROM financial_write_requests f WHERE f.tenant_id=d.tenant_id AND f.operation='investment.import.commit' AND f.request_key=d.request_key)`, t, u).Scan(&count); err != nil {
			return empty, err
		}
		if count >= 50 {
			return empty, d.ErrInvestmentLimit
		}
	}
	// A draft must not steal another user's request key or another draft's key.
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_batch_drafts WHERE tenant_id=$1 AND request_key=$2 AND NOT(actor_id=$3 AND id=$4))`, t, b.RequestKey, u, w.ID).Scan(&exists); err != nil {
		return empty, err
	}
	if exists {
		return empty, d.ErrInvestmentConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO investment_batch_drafts(tenant_id,actor_id,id,import_id,version,request_key,payload_hash,data) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
 ON CONFLICT(tenant_id,actor_id,id) DO UPDATE SET import_id=EXCLUDED.import_id,version=EXCLUDED.version,request_key=EXCLUDED.request_key,payload_hash=EXCLUDED.payload_hash,data=EXCLUDED.data,preview=NULL,updated_at=clock_timestamp()`, t, u, w.ID, b.ImportID, w.ExpectedVersion+1, b.RequestKey, hash, raw)
	if err != nil {
		return empty, err
	}
	out, err := readBatchDraft(ctx, tx, t, u, w.ID)
	if err != nil {
		return empty, err
	}
	return out, tx.Commit(ctx)
}
func (r *InvestmentRepo) RunBatchDraft(ctx context.Context, t, u, id string, version int64, hash string, commit bool) (inv.BatchDraft, error) {
	var empty inv.BatchDraft
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = lockInvestment(ctx, tx, t); err != nil {
		return empty, err
	}
	if err = authorizeBatchDraft(ctx, tx, t, u); err != nil {
		return empty, err
	}
	draft, err := readBatchDraft(ctx, tx, t, u, id)
	if err != nil {
		return empty, err
	}
	if draft.Version != version {
		return empty, d.ErrInvestmentConflict
	}
	if commit && (draft.Preview == nil || draft.Preview.Hash != hash) {
		return empty, d.ErrInvestmentPrecondition
	}
	result, err := r.importBatchTx(ctx, tx, t, u, draft.Batch, hash, commit)
	if err != nil {
		return empty, err
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 32*1024*1024 {
		return empty, d.ErrInvestmentLimit
	}
	if _, err = tx.Exec(ctx, `UPDATE investment_batch_drafts SET preview=$4,updated_at=clock_timestamp() WHERE tenant_id=$1 AND actor_id=$2 AND id=$3`, t, u, id, raw); err != nil {
		return empty, err
	}
	out, err := readBatchDraft(ctx, tx, t, u, id)
	if err != nil {
		return empty, err
	}
	return out, tx.Commit(ctx)
}
func (r *InvestmentRepo) DeleteBatchDraft(ctx context.Context, t, u, id string, version int64) error {
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = lockInvestment(ctx, tx, t); err != nil {
		return err
	}
	if err = authorizeBatchDraft(ctx, tx, t, u); err != nil {
		return err
	}
	draft, err := readBatchDraft(ctx, tx, t, u, id)
	if errors.Is(err, d.ErrInvestmentNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if draft.Version != version {
		return d.ErrInvestmentConflict
	}
	var used bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM financial_write_requests WHERE tenant_id=$1 AND operation='investment.import.commit' AND request_key=$2)`, t, draft.Batch.RequestKey).Scan(&used); err != nil {
		return err
	}
	if used {
		return d.ErrInvestmentPrecondition
	}
	if _, err = tx.Exec(ctx, `DELETE FROM investment_batch_drafts WHERE tenant_id=$1 AND actor_id=$2 AND id=$3`, t, u, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *InvestmentRepo) ListBatchDrafts(ctx context.Context, t, u, after string) ([]inv.BatchDraftSummary, error) {
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = authorizeBatchDraft(ctx, tx, t, u); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT d.id,d.import_id,d.version,jsonb_array_length(d.data->'Entries'),EXISTS(SELECT 1 FROM financial_write_requests f WHERE f.tenant_id=d.tenant_id AND f.operation='investment.import.commit' AND f.request_key=d.request_key AND f.payload_hash=d.payload_hash),d.updated_at,EXISTS(SELECT 1 FROM investment_batch_cancellations c WHERE c.tenant_id=d.tenant_id AND c.batch_request_key=d.request_key) FROM investment_batch_drafts d WHERE d.tenant_id=$1 AND d.actor_id=$2 AND ($3='' OR d.id>NULLIF($3,'')::uuid) ORDER BY d.id LIMIT 50`, t, u, after)
	if err != nil {
		return nil, err
	}
	out := []inv.BatchDraftSummary{}
	for rows.Next() {
		var item inv.BatchDraftSummary
		if err = rows.Scan(&item.ID, &item.ImportID, &item.Version, &item.Rows, &item.Committed, &item.UpdatedAt, &item.Cancelled); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
