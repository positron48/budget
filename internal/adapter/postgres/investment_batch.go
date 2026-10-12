package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

func (r *InvestmentRepo) ImportBatch(ctx context.Context, tenant, actor string, b inv.ImportBatch, expectedHash string, commit bool) (inv.ImportBatchResult, error) {
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return inv.ImportBatchResult{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	result, err := r.importBatchTx(ctx, tx, tenant, actor, b, expectedHash, commit)
	if err != nil {
		return result, err
	}
	if commit {
		return result, tx.Commit(ctx)
	}
	return result, tx.Rollback(ctx)
}
func (r *InvestmentRepo) importBatchTx(ctx context.Context, tx pgx.Tx, tenant, actor string, b inv.ImportBatch, expectedHash string, commit bool) (inv.ImportBatchResult, error) {
	var empty inv.ImportBatchResult
	b, err := inv.NormalizeImportBatch(tenant, actor, b)
	if err != nil {
		return empty, err
	}
	encoded, err := json.Marshal(b)
	if err != nil || len(encoded) > 20*1024*1024 {
		return empty, d.ErrInvestmentLimit
	}
	hash := investmentBatchHash(tenant, actor, b)
	if commit && expectedHash != hash {
		return empty, d.ErrInvestmentConflict
	}
	if err = lockInvestment(ctx, tx, tenant); err != nil {
		return empty, err
	}
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, tenant, actor).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && role != "owner" && role != "admin" {
		return empty, d.ErrInvestmentForbidden
	}
	if err != nil {
		return empty, err
	}
	var savedHash string
	var saved []byte
	err = tx.QueryRow(ctx, `SELECT payload_hash,result FROM financial_write_requests WHERE tenant_id=$1 AND operation='investment.import.commit' AND request_key=$2`, tenant, b.RequestKey).Scan(&savedHash, &saved)
	if err == nil {
		if savedHash != hash {
			return empty, d.ErrInvestmentConflict
		}
		if err = json.Unmarshal(saved, &empty); err != nil {
			return empty, err
		}
		return empty, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	// A key owned by a durable review cannot bypass its latest version through
	// the legacy body-based RPC. The draft retains its key until committed.
	var draftActor, draftHash string
	var draftPreview []byte
	err = tx.QueryRow(ctx, `SELECT actor_id,payload_hash,preview FROM investment_batch_drafts WHERE tenant_id=$1 AND request_key=$2`, tenant, b.RequestKey).Scan(&draftActor, &draftHash, &draftPreview)
	if err == nil {
		if draftActor != actor || draftHash != hash {
			return empty, d.ErrInvestmentConflict
		}
		if commit {
			var reviewed inv.ImportBatchResult
			if len(draftPreview) == 0 || json.Unmarshal(draftPreview, &reviewed) != nil || reviewed.Hash != hash {
				return empty, d.ErrInvestmentPrecondition
			}
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	var version int64
	var preview string
	if err = tx.QueryRow(ctx, `SELECT version,preview_hash FROM investment_imports WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, b.ImportID).Scan(&version, &preview); err != nil {
		return empty, investmentError(err)
	}
	if version != b.ExpectedImportVersion || preview != b.PreviewHash {
		return empty, d.ErrInvestmentConflict
	}
	records := make([]int32, len(b.Entries))
	for i, entry := range b.Entries {
		records[i] = int32(entry.Record)
	}
	type sourceRow struct {
		account string
		version int64
		used    bool
	}
	sources := map[int]sourceRow{}
	rows, err := tx.Query(ctx, `SELECT r.record,r.account_id,r.version,EXISTS(SELECT 1 FROM investment_source_links s WHERE s.tenant_id=r.tenant_id AND s.import_id=r.import_id AND s.record=r.record) FROM investment_import_rows r WHERE r.tenant_id=$1 AND r.import_id=$2 AND r.record=ANY($3::int[]) ORDER BY r.record FOR UPDATE OF r`, tenant, b.ImportID, records)
	if err != nil {
		return empty, err
	}
	for rows.Next() {
		var record int
		var v sourceRow
		if err = rows.Scan(&record, &v.account, &v.version, &v.used); err != nil {
			rows.Close()
			return empty, err
		}
		sources[record] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, err
	}
	if len(sources) != len(b.Entries) {
		return empty, d.ErrInvestmentNotFound
	}
	bindings := map[string]string{}
	rows, err = tx.Query(ctx, `SELECT account_id,ledger_id FROM investment_import_ledger_bindings WHERE tenant_id=$1 AND import_id=$2`, tenant, b.ImportID)
	if err != nil {
		return empty, err
	}
	for rows.Next() {
		var account, ledger string
		if err = rows.Scan(&account, &ledger); err != nil {
			rows.Close()
			return empty, err
		}
		bindings[account] = ledger
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, err
	}
	for _, entry := range b.Entries {
		source := sources[entry.Record]
		if source.used || source.version != entry.ExpectedRowVersion {
			return empty, d.ErrInvestmentConflict
		}
		if bound := bindings[source.account]; bound != "" && bound != entry.Operation.LedgerID {
			return empty, d.ErrInvestmentConflict
		}
		bindings[source.account] = entry.Operation.LedgerID
	}
	keys := make([]string, 0, len(b.ExpectedLedgerVersions))
	for id := range b.ExpectedLedgerVersions {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	type ledgerWork struct {
		ledger      d.InvestmentLedger
		operations  []d.InvestmentOperation
		next, added int64
		dirty       time.Time
		replay      inv.ReplayResult
	}
	books := map[string]*ledgerWork{}
	for _, id := range keys {
		ledger, err := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id))
		if err != nil {
			return empty, err
		}
		if ledger.Archived {
			return empty, d.ErrInvestmentPrecondition
		}
		if ledger.Version != b.ExpectedLedgerVersions[id] {
			return empty, d.ErrInvestmentConflict
		}
		work := &ledgerWork{ledger: ledger}
		if err = tx.QueryRow(ctx, `SELECT next_sequence FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&work.next); err != nil {
			return empty, err
		}
		if work.operations, err = loadInvestmentOperations(ctx, tx, tenant, id); err != nil {
			return empty, err
		}
		books[id] = work
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	result := inv.ImportBatchResult{Hash: hash, Committed: commit, Operations: []d.InvestmentOperation{}, Snapshots: []d.InvestmentSnapshot{}}
	namespace, err := uuid.Parse(tenant)
	if err != nil {
		return empty, d.ErrInvestmentInvalid
	}
	for _, entry := range b.Entries {
		op := entry.Operation
		if err = verifyOperationLotConversion(ctx, tx, tenant, op, commit); err != nil {
			return result, err
		}
		work := books[op.LedgerID]
		op.ID = uuid.NewSHA1(namespace, []byte("investment.import.commit:"+actor+":"+b.RequestKey+":"+strconv.Itoa(entry.Record))).String()
		op.Sequence = work.next + work.added
		op.Version, op.Status, op.CreatedAt = 1, "posted", now
		op.Source = &d.InvestmentSource{ImportID: b.ImportID, Record: entry.Record, ExpectedVersion: entry.ExpectedRowVersion, ReviewReason: entry.ReviewReason}
		work.operations = append(work.operations, op)
		work.added++
		if work.dirty.IsZero() || op.OccurredAt.Before(work.dirty) {
			work.dirty = op.OccurredAt
		}
		result.Operations = append(result.Operations, op)
	}
	overrides := map[string]investmentReplayInput{}
	dirty := time.Time{}
	for _, id := range keys {
		work := books[id]
		work.ledger.Version += work.added
		overrides[id] = investmentReplayInput{work.ledger, work.operations}
		if dirty.IsZero() || work.dirty.Before(dirty) {
			dirty = work.dirty
		}
	}
	connected, err := readConnectedInvestmentReplay(ctx, tx, tenant, keys, overrides, now, nil)
	if err != nil {
		return empty, err
	}
	for _, id := range keys {
		books[id].replay = connected.Ledgers[id]
		result.Snapshots = append(result.Snapshots, books[id].replay.Snapshot)
	}

	if !commit {
		return result, nil
	}
	// Persist only after every affected journal has passed its full replay.
	heads, revisions, links := [][]any{}, [][]any{}, [][]any{}
	for i, op := range result.Operations {
		raw, err := json.Marshal(op)
		if err != nil {
			return empty, err
		}
		heads = append(heads, []any{op.ID, tenant, op.LedgerID, op.Kind, op.OccurredAt, op.SettlementAt, op.Sequence, op.Version, op.Status, raw, op.CreatedAt})
		revisions = append(revisions, []any{tenant, op.LedgerID, op.ID, int64(1), "create", raw, actor, ""})
		entry := b.Entries[i]
		links = append(links, []any{tenant, b.ImportID, entry.Record, entry.ExpectedRowVersion, sources[entry.Record].account, op.LedgerID, op.ID, entry.ReviewReason, actor})
	}
	for _, copy := range []struct {
		table   string
		columns []string
		rows    [][]any
	}{
		{"investment_operations", []string{"id", "tenant_id", "ledger_id", "kind", "occurred_at", "settlement_at", "sequence", "version", "status", "data", "created_at"}, heads},
		{"investment_operation_revisions", []string{"tenant_id", "ledger_id", "operation_id", "version", "action", "after_data", "actor_id", "reason"}, revisions},
		{"investment_source_links", []string{"tenant_id", "import_id", "record", "source_version", "account_id", "ledger_id", "operation_id", "review_reason", "actor_id"}, links},
	} {
		if _, err = tx.CopyFrom(ctx, pgx.Identifier{copy.table}, copy.columns, pgx.CopyFromRows(copy.rows)); err != nil {
			return empty, err
		}
	}
	for account, ledger := range bindings {
		if _, err = tx.Exec(ctx, `INSERT INTO investment_import_ledger_bindings(tenant_id,import_id,account_id,ledger_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, tenant, b.ImportID, account, ledger); err != nil {
			return empty, err
		}
	}
	for _, id := range keys {
		work := books[id]
		if err = replaceInvestmentLegs(ctx, tx, tenant, id, work.replay); err != nil {
			return empty, err
		}
		if _, err = tx.Exec(ctx, `UPDATE investment_ledgers SET version=$3,next_sequence=$4 WHERE tenant_id=$1 AND id=$2`, tenant, id, work.ledger.Version, work.next+work.added); err != nil {
			return empty, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO investment_recalculation_jobs(tenant_id,ledger_id,journal_revision,dirty_from) VALUES($1,$2,$3,$4)`, tenant, id, work.ledger.Version, work.dirty); err != nil {
			return empty, err
		}
	}
	skip := map[string]bool{}
	for _, id := range keys {
		skip[id] = true
	}
	if err = replaceConnectedInvestmentDependents(ctx, tx, tenant, connected, skip, dirty); err != nil {
		return empty, err
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 32*1024*1024 {
		return empty, d.ErrInvestmentLimit
	}
	if _, err = tx.Exec(ctx, `INSERT INTO financial_write_requests(tenant_id,operation,request_key,payload_hash,result) VALUES($1,'investment.import.commit',$2,$3,$4)`, tenant, b.RequestKey, hash, raw); err != nil {
		return empty, err
	}
	return result, nil
}
