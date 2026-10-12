package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"sort"
	"time"
)

// CancelBatch voids the current heads of exactly the original receipt's members.
// The shared journal lock serializes posting, amendments and cancellation.
func (r *InvestmentRepo) CancelBatch(ctx context.Context, t, u string, c inv.BatchCancellation, expectedHash string, commit bool) (inv.BatchCancellationResult, error) {
	var empty inv.BatchCancellationResult
	if err := inv.ValidateBatchCancellation(c); err != nil {
		return empty, err
	}
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
	draft, err := readBatchDraft(ctx, tx, t, u, c.DraftID)
	if err != nil {
		return empty, err
	}
	if draft.Version != c.ExpectedDraftVersion {
		return empty, d.ErrInvestmentConflict
	}
	if draft.Preview == nil || !draft.Preview.Committed {
		return empty, d.ErrInvestmentPrecondition
	}
	requestHash := investmentHash(struct {
		Tenant, Actor string
		Input         inv.BatchCancellation
	}{t, u, c})
	var savedHash string
	var saved []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,result FROM investment_batch_cancellations WHERE tenant_id=$1 AND batch_request_key=$2`, t, draft.Batch.RequestKey).Scan(&savedHash, &saved)
	if err == nil {
		if savedHash != requestHash {
			return empty, d.ErrInvestmentConflict
		}
		if err = json.Unmarshal(saved, &empty); err != nil {
			return empty, err
		}
		if commit && expectedHash != empty.Hash {
			return inv.BatchCancellationResult{}, d.ErrInvestmentConflict
		}
		return empty, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	var used bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_batch_cancellations WHERE tenant_id=$1 AND request_key=$2)`, t, c.RequestKey).Scan(&used); err != nil {
		return empty, err
	}
	if used {
		return empty, d.ErrInvestmentConflict
	}
	members := map[string]d.InvestmentOperation{}
	ledgerIDs := map[string]bool{}
	if len(draft.Preview.Operations) == 0 || len(draft.Preview.Operations) > inv.MaxBatchOperations {
		return empty, d.ErrInvestmentPrecondition
	}
	for _, op := range draft.Preview.Operations {
		if _, duplicate := members[op.ID]; duplicate || op.TenantID != t || op.Source == nil || op.Source.ImportID != draft.Batch.ImportID || (op.TransferID != "" || op.BudgetTransferID != "") {
			return empty, d.ErrInvestmentPrecondition
		}
		members[op.ID] = op
		ledgerIDs[op.LedgerID] = true
	}
	keys := make([]string, 0, len(ledgerIDs))
	for id := range ledgerIDs {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	type work struct {
		ledger  d.InvestmentLedger
		replay  inv.ReplayResult
		dirty   time.Time
		changed int64
	}
	books := map[string]*work{}
	overrides := map[string]investmentReplayInput{}
	dirty := time.Time{}
	versions := map[string]int64{}
	heads := map[string]int64{}
	revisions := [][]any{}
	type update struct {
		ID      string          `json:"id"`
		Version int64           `json:"version"`
		Data    json.RawMessage `json:"data"`
	}
	updates := []update{}
	result := inv.BatchCancellationResult{Reason: c.Reason, Operations: []d.InvestmentOperation{}, AlreadyVoided: []string{}, Snapshots: []d.InvestmentSnapshot{}}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, id := range keys {
		ledger, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, id))
		if e != nil {
			return empty, e
		}
		if ledger.Archived {
			return empty, d.ErrInvestmentPrecondition
		}
		versions[id] = ledger.Version
		ops, e := loadInvestmentOperations(ctx, tx, t, id)
		if e != nil {
			return empty, e
		}
		w := &work{ledger: ledger}
		books[id] = w
		for i, old := range ops {
			original, ok := members[old.ID]
			if !ok {
				continue
			}
			if original.LedgerID != id || old.Source == nil || old.Source.ImportID != original.Source.ImportID || old.Source.Record != original.Source.Record || (old.TransferID != "" || old.BudgetTransferID != "") {
				return empty, d.ErrInvestmentPrecondition
			}
			heads[old.ID] = old.Version
			if old.Status == "voided" {
				result.AlreadyVoided = append(result.AlreadyVoided, old.ID)
				continue
			}
			if old.Status != "posted" {
				return empty, d.ErrInvestmentPrecondition
			}
			next := old
			next.Version++
			next.Status = "voided"
			next.ActorID = u
			before, e := json.Marshal(old)
			if e != nil {
				return empty, e
			}
			after, e := json.Marshal(next)
			if e != nil {
				return empty, e
			}
			revisions = append(revisions, []any{t, id, old.ID, next.Version, "void", before, after, u, c.Reason})
			updates = append(updates, update{next.ID, next.Version, after})
			ops[i] = next
			w.changed++
			if w.dirty.IsZero() || old.OccurredAt.Before(w.dirty) {
				w.dirty = old.OccurredAt
			}
			result.Operations = append(result.Operations, next)
		}
		w.ledger.Version += w.changed
		overrides[id] = investmentReplayInput{w.ledger, ops}
		if w.changed > 0 && (dirty.IsZero() || w.dirty.Before(dirty)) {
			dirty = w.dirty
		}
	}
	if len(heads) != len(members) {
		return empty, d.ErrInvestmentPrecondition
	}
	connected, e := readConnectedInvestmentReplay(ctx, tx, t, keys, overrides, now, nil)
	if e != nil {
		return empty, e
	}
	for _, id := range keys {
		books[id].replay = connected.Ledgers[id]
		result.Snapshots = append(result.Snapshots, books[id].replay.Snapshot)
	}
	result.Hash = investmentHash(struct {
		Request, Batch string
		Ledgers, Heads map[string]int64
	}{requestHash, draft.Batch.RequestKey, versions, heads})
	if !commit {
		return result, tx.Rollback(ctx)
	}
	if expectedHash != result.Hash {
		return empty, d.ErrInvestmentConflict
	}
	if len(updates) > 0 {
		raw, e := json.Marshal(updates)
		if e != nil {
			return empty, e
		}
		tag, e := tx.Exec(ctx, `UPDATE investment_operations o SET version=v.version,status='voided',data=v.data FROM jsonb_to_recordset($2::jsonb) AS v(id uuid,version bigint,data jsonb) WHERE o.tenant_id=$1 AND o.id=v.id`, t, raw)
		if e != nil {
			return empty, e
		}
		if tag.RowsAffected() != int64(len(updates)) {
			return empty, d.ErrInvestmentConflict
		}
		if _, err = tx.CopyFrom(ctx, pgx.Identifier{"investment_operation_revisions"}, []string{"tenant_id", "ledger_id", "operation_id", "version", "action", "before_data", "after_data", "actor_id", "reason"}, pgx.CopyFromRows(revisions)); err != nil {
			return empty, err
		}
	}
	for _, id := range keys {
		w := books[id]
		if w.changed == 0 {
			continue
		}
		if err = replaceInvestmentLegs(ctx, tx, t, id, w.replay); err != nil {
			return empty, err
		}
		if _, err = tx.Exec(ctx, `UPDATE investment_ledgers SET version=$3 WHERE tenant_id=$1 AND id=$2`, t, id, w.ledger.Version); err != nil {
			return empty, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO investment_recalculation_jobs(tenant_id,ledger_id,journal_revision,dirty_from) VALUES($1,$2,$3,$4)`, t, id, w.ledger.Version, w.dirty); err != nil {
			return empty, err
		}
	}
	result.Cancelled = true
	result.CancelledAt = now
	if !dirty.IsZero() {
		skip := map[string]bool{}
		for _, id := range keys {
			skip[id] = true
		}
		if err = replaceConnectedInvestmentDependents(ctx, tx, t, connected, skip, dirty); err != nil {
			return empty, err
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return empty, err
	}
	if len(raw) > 32*1024*1024 {
		return empty, d.ErrInvestmentLimit
	}
	if _, err = tx.Exec(ctx, `INSERT INTO investment_batch_cancellations(tenant_id,batch_request_key,request_key,request_hash,draft_id,actor_id,reason,result) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, t, draft.Batch.RequestKey, c.RequestKey, requestHash, c.DraftID, u, c.Reason, raw); err != nil {
		return empty, err
	}
	return result, tx.Commit(ctx)
}
