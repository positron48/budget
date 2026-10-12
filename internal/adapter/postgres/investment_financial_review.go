package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	inv "github.com/positron48/budget/internal/usecase/investment"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"time"
)

func (r *InvestmentImportRepo) PreviewFinancialImport(ctx context.Context, t, u string, request imp.FinancialReviewRequest) (imp.FinancialReview, error) {
	var out imp.FinancialReview
	// All source assignments, consumed rows and ledger versions come from one snapshot.
	tx, err := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = authorizeBatchDraft(ctx, tx, t, u); err != nil {
		return out, err
	}
	var csv []byte
	var version int64
	var stagingHash string
	if err = tx.QueryRow(ctx, `SELECT original_csv,version,preview_hash FROM investment_imports WHERE tenant_id=$1 AND id=$2`, t, request.ImportID).Scan(&csv, &version, &stagingHash); err != nil {
		return out, importError(err)
	}
	if version != request.ExpectedImportVersion {
		return out, imp.ErrConflict
	}
	report, err := imp.MapSnowballFinancial(bytes.NewReader(csv), request.Mapping)
	if err != nil {
		return out, fmt.Errorf("%w: %s", imp.ErrInvalid, err)
	}
	type state struct {
		version                   int64
		account, bound, sourceKey string
		used                      bool
	}
	states := map[int]state{}
	rows, err := tx.Query(ctx, `SELECT r.record,r.version,r.account_id,a.source_key,COALESCE(b.ledger_id::text,''),EXISTS(SELECT 1 FROM investment_source_links s WHERE s.tenant_id=r.tenant_id AND s.import_id=r.import_id AND s.record=r.record) FROM investment_import_rows r JOIN investment_import_accounts a ON a.tenant_id=r.tenant_id AND a.import_id=r.import_id AND a.id=r.account_id LEFT JOIN investment_import_ledger_bindings b ON b.tenant_id=r.tenant_id AND b.import_id=r.import_id AND b.account_id=r.account_id WHERE r.tenant_id=$1 AND r.import_id=$2 ORDER BY r.record`, t, request.ImportID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var record int
		var v state
		if err = rows.Scan(&record, &v.version, &v.account, &v.sourceKey, &v.bound, &v.used); err != nil {
			rows.Close()
			return out, err
		}
		states[record] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, row := range report.Rows {
		if row.Operation != nil && !seen[row.Operation.LedgerID] {
			ids = append(ids, row.Operation.LedgerID)
			seen[row.Operation.LedgerID] = true
		}
	}
	versions := map[string]int64{}
	rows, err = tx.Query(ctx, `SELECT id,version FROM investment_ledgers WHERE tenant_id=$1 AND id=ANY($2::uuid[])`, t, ids)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id string
		var v int64
		if err = rows.Scan(&id, &v); err != nil {
			rows.Close()
			return out, err
		}
		versions[id] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	// A proposed source account cannot be split across journals accidentally.
	proposed := map[string]string{}
	conflicting := map[string]bool{}
	for _, row := range report.Rows {
		if row.Operation == nil {
			continue
		}
		account := states[row.Record].account
		id := row.Operation.LedgerID
		if previous := proposed[account]; previous != "" && previous != id {
			conflicting[account] = true
		}
		proposed[account] = id
	}
	out.Total = len(report.Rows)
	rowVersions := map[int]int64{}
	for i := range report.Rows {
		row := &report.Rows[i]
		s, ok := states[row.Record]
		rowVersions[row.Record] = s.version
		if !ok {
			row.Issues = append(row.Issues, "source_row_missing")
		}
		if evidence, ok := request.Mapping.CashEvidence[row.Record]; ok && s.sourceKey != "tinvest:"+evidence.Operation.AccountID {
			row.Issues = append(row.Issues, "broker_account_mapping_mismatch")
		}
		if evidence, ok := request.Mapping.IncomeEvidence[row.Record]; ok && s.sourceKey != "tinvest:"+evidence.Credit.AccountID {
			row.Issues = append(row.Issues, "broker_account_mapping_mismatch")
		}
		if evidence, ok := request.Mapping.TradeEvidence[row.Record]; ok && s.sourceKey != "tinvest:"+evidence.BrokerAccountID {
			row.Issues = append(row.Issues, "broker_account_mapping_mismatch")
		}
		if evidence, ok := request.Mapping.SplitEvidence[row.Record]; ok && evidence.BrokerAccountID != "" && s.sourceKey != "tinvest:"+evidence.BrokerAccountID {
			row.Issues = append(row.Issues, "broker_account_mapping_mismatch")
		}
		if s.used {
			row.Issues = append(row.Issues, "source_row_already_consumed")
		}
		if row.Operation != nil {
			if row.Operation.OccurredAt.After(time.Now().UTC().Add(5 * time.Minute)) {
				row.Issues = append(row.Issues, "future_operation_requires_review")
			}
			if versions[row.Operation.LedgerID] == 0 {
				row.Issues = append(row.Issues, "ledger_unavailable")
			}
			if s.bound != "" && s.bound != row.Operation.LedgerID || conflicting[s.account] {
				row.Issues = append(row.Issues, "source_account_ledger_conflict")
			}
		}
		if len(row.Issues) > 0 {
			row.Operation = nil
		}
		if row.Operation != nil {
			out.Ready++
		} else {
			out.Unresolved++
		}
	}
	out.Hash = investmentHash(struct {
		Tenant, Actor, Import string
		Version               int64
		Report                imp.FinancialMappingReport
		RowVersions           map[int]int64
		LedgerVersions        map[string]int64
	}{t, u, request.ImportID, version, report, rowVersions, versions})
	evidence, err := json.Marshal(struct {
		Version    int
		ReviewHash string
		Mapping    imp.FinancialMapping
	}{1, out.Hash, request.Mapping})
	if err != nil {
		return out, err
	}
	if len(evidence) > imp.MaxMappingBytes {
		return out, imp.ErrInvalid
	}
	b := inv.ImportBatch{ImportID: request.ImportID, ExpectedImportVersion: version, PreviewHash: stagingHash, RequestKey: request.RequestKey, PreparationEvidenceJSON: string(evidence), ExpectedLedgerVersions: map[string]int64{}, Entries: []inv.ImportBatchEntry{}}
	end := request.Offset + request.Limit
	if end > len(report.Rows) {
		end = len(report.Rows)
	}
	out.Rows = []imp.FinancialCandidate{}
	if request.Offset < len(report.Rows) {
		out.Rows = report.Rows[request.Offset:end]
	}
	for _, row := range out.Rows {
		if row.Operation == nil {
			continue
		}
		b.ExpectedLedgerVersions[row.Operation.LedgerID] = versions[row.Operation.LedgerID]
		b.Entries = append(b.Entries, inv.ImportBatchEntry{Record: row.Record, ExpectedRowVersion: states[row.Record].version, ReviewReason: row.ReviewReason, Operation: *row.Operation})
	}
	if len(b.Entries) > 0 {
		b, err = inv.NormalizeImportBatch(t, u, b)
		if err != nil {
			return out, fmt.Errorf("%w: candidate batch cannot be normalized", imp.ErrInvalid)
		}
		out.Batch = &b
	}
	return out, tx.Commit(ctx)
}
