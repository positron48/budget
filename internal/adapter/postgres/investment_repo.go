package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

type InvestmentRepo struct {
	pool            *Pool
	exportTenantIDs []string
}

func NewInvestmentRepo(p *Pool) *InvestmentRepo { return &InvestmentRepo{pool: p} }
func investmentError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return d.ErrInvestmentNotFound
	}
	return err
}
func investmentHash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func lockInvestment(ctx context.Context, tx pgx.Tx, tenant string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "investment-journal:"+tenant)
	return err
}
func investmentRetry(ctx context.Context, tx pgx.Tx, tenant, namespace, key, hash string) (string, error) {
	var saved, id string
	err := tx.QueryRow(ctx, `SELECT payload_hash,result->>'id' FROM financial_write_requests WHERE tenant_id=$1 AND operation=$2 AND request_key=$3`, tenant, namespace, key).Scan(&saved, &id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if hash != saved {
		return "", d.ErrInvestmentConflict
	}
	return id, nil
}
func saveInvestmentRequest(ctx context.Context, tx pgx.Tx, t, namespace, key, hash, id string) error {
	_, err := tx.Exec(ctx, `INSERT INTO financial_write_requests(tenant_id,operation,request_key,payload_hash,result) VALUES($1,$2,$3,$4,jsonb_build_object('id',$5::text))`, t, namespace, key, hash, id)
	return err
}

const ledgerColumns = `id,tenant_id,name,started_at,version,created_at,broker,account_type,comment,default_currency,archived,metadata_version,portfolio_tracking`

func scanLedger(row pgx.Row) (d.InvestmentLedger, error) {
	var l d.InvestmentLedger
	err := row.Scan(&l.ID, &l.TenantID, &l.Name, &l.StartedAt, &l.Version, &l.CreatedAt, &l.Broker, &l.AccountType, &l.Comment, &l.DefaultCurrency, &l.Archived, &l.MetadataVersion, &l.PortfolioTracking)
	return l, investmentError(err)
}
func (r *InvestmentRepo) CreateLedger(ctx context.Context, l d.InvestmentLedger, user, key string) (d.InvestmentLedger, error) {
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return l, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockInvestment(ctx, tx, l.TenantID); err != nil {
		return l, err
	}
	if l.AccountType == "" {
		l.AccountType = "other"
	}
	if l.DefaultCurrency == "" {
		l.DefaultCurrency = "RUB"
	}
	hash := investmentHash(l)
	id, err := investmentRetry(ctx, tx, l.TenantID, "investment.ledger.create", key, hash)
	if err != nil {
		return l, err
	}
	if id != "" {
		return scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, l.TenantID, id))
	}
	result, err := scanLedger(tx.QueryRow(ctx, `INSERT INTO investment_ledgers(tenant_id,name,started_at,created_by,broker,account_type,comment,default_currency,portfolio_tracking) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+ledgerColumns, l.TenantID, l.Name, l.StartedAt, user, l.Broker, l.AccountType, l.Comment, l.DefaultCurrency, l.PortfolioTracking))
	if err != nil {
		return l, err
	}
	if err = recordLedgerRevision(ctx, tx, result, user, "create", "", nil); err != nil {
		return l, err
	}
	if err = saveInvestmentRequest(ctx, tx, l.TenantID, "investment.ledger.create", key, hash, result.ID); err != nil {
		return l, err
	}
	return result, tx.Commit(ctx)
}
func (r *InvestmentRepo) ListLedgers(ctx context.Context, t string, limit, offset int) ([]d.InvestmentLedger, error) {
	rows, err := r.pool.DB.Query(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3`, t, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []d.InvestmentLedger{}
	for rows.Next() {
		v, err := scanLedger(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func scanInvestmentOperation(row pgx.Row) (d.InvestmentOperation, error) {
	var raw []byte
	var o d.InvestmentOperation
	if err := row.Scan(&raw); err != nil {
		return o, investmentError(err)
	}
	err := json.Unmarshal(raw, &o)
	return o, err
}
func loadInvestmentOperations(ctx context.Context, tx pgx.Tx, t, id string) ([]d.InvestmentOperation, error) {
	rows, err := tx.Query(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2 ORDER BY occurred_at,sequence LIMIT $3`, t, id, inv.MaxOperations+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []d.InvestmentOperation{}
	for rows.Next() {
		v, err := scanInvestmentOperation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	if len(result) > inv.MaxOperations {
		return nil, d.ErrInvestmentLimit
	}
	return result, rows.Err()
}

// Performance needs only operations inside its exact cutoff interval. The same
// bound and decoder as full replay apply; ordering and date-only timestamps are
// unchanged. Full journal replay continues to use loadInvestmentOperations.
const investmentOperationsPeriodSQL = `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2 AND occurred_at>$3 AND occurred_at<=$4 ORDER BY occurred_at,sequence LIMIT $5`

func loadInvestmentOperationsInPeriod(ctx context.Context, tx pgx.Tx, tenant, ledger string, after, through time.Time) ([]d.InvestmentOperation, error) {
	rows, err := tx.Query(ctx, investmentOperationsPeriodSQL, tenant, ledger, after, through, inv.MaxOperations+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []d.InvestmentOperation{}
	for rows.Next() {
		v, e := scanInvestmentOperation(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, v)
	}
	if len(result) > inv.MaxOperations {
		return nil, d.ErrInvestmentLimit
	}
	return result, rows.Err()
}

func (r *InvestmentRepo) Snapshot(ctx context.Context, t, id string, at time.Time) (d.InvestmentSnapshot, error) {
	tx, err := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return d.InvestmentSnapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	l, err := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, t, id))
	if err != nil {
		return d.InvestmentSnapshot{}, err
	}
	ops, err := loadInvestmentOperations(ctx, tx, t, id)
	if err != nil {
		return d.InvestmentSnapshot{}, err
	}
	result, err := replayInvestmentLedgerTx(ctx, tx, t, l, ops, at)
	if err != nil {
		return result.Snapshot, err
	}
	return result.Snapshot, tx.Commit(ctx)
}
func (r *InvestmentRepo) ListOperations(ctx context.Context, t, id string, limit, offset int) ([]d.InvestmentOperation, error) {
	var exists bool
	if err := r.pool.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_ledgers WHERE tenant_id=$1 AND id=$2)`, t, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, d.ErrInvestmentNotFound
	}
	rows, err := r.pool.DB.Query(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2 ORDER BY occurred_at DESC,sequence DESC LIMIT $3 OFFSET $4`, t, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []d.InvestmentOperation{}
	for rows.Next() {
		v, err := scanInvestmentOperation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (r *InvestmentRepo) Mutate(ctx context.Context, t, user string, m inv.Mutation) (d.InvestmentOperation, error) {
	empty := d.InvestmentOperation{}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockInvestment(ctx, tx, t); err != nil {
		return empty, err
	}
	result, err := r.mutateInvestmentTx(ctx, tx, t, user, m, false)
	if err != nil {
		return empty, err
	}
	return result, tx.Commit(ctx)
}
func (r *InvestmentRepo) mutateInvestmentTx(ctx context.Context, tx pgx.Tx, t, user string, m inv.Mutation, transfer bool) (d.InvestmentOperation, error) {
	return r.mutateInvestmentFactsTx(ctx, tx, t, user, m, transfer, false)
}
func (r *InvestmentRepo) mutateInvestmentFactsTx(ctx context.Context, tx pgx.Tx, t, user string, m inv.Mutation, transfer, deferReplay bool) (d.InvestmentOperation, error) {
	empty := d.InvestmentOperation{}
	hashInput := m
	hashInput.RequestKey = ""
	hash := investmentHash(hashInput)
	retry, err := investmentRetry(ctx, tx, t, "investment.operation.mutate", m.RequestKey, hash)
	if err != nil {
		return empty, err
	}
	if retry != "" {
		return scanInvestmentOperation(tx.QueryRow(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2 AND id=$3`, t, m.Operation.LedgerID, retry))
	}
	var sourceAccount string
	if m.Source != nil {
		// Same import -> row lock order as remapping. A financial source link
		// and its account mapping must describe one consistent reviewed version.
		var version int64
		if err = tx.QueryRow(ctx, `SELECT version FROM investment_imports WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, m.Source.ImportID).Scan(&version); err != nil {
			return empty, investmentError(err)
		}
		if err = tx.QueryRow(ctx, `SELECT account_id,version FROM investment_import_rows WHERE tenant_id=$1 AND import_id=$2 AND record=$3 FOR UPDATE`, t, m.Source.ImportID, m.Source.Record).Scan(&sourceAccount, &version); err != nil {
			return empty, investmentError(err)
		}
		var used bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_source_links WHERE tenant_id=$1 AND import_id=$2 AND record=$3)`, t, m.Source.ImportID, m.Source.Record).Scan(&used); err != nil {
			return empty, err
		}
		if used || version != m.Source.ExpectedVersion {
			return empty, d.ErrInvestmentConflict
		}
		var bound string
		err = tx.QueryRow(ctx, `SELECT ledger_id FROM investment_import_ledger_bindings WHERE tenant_id=$1 AND import_id=$2 AND account_id=$3`, t, m.Source.ImportID, sourceAccount).Scan(&bound)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return empty, err
		}
		if bound != "" && bound != m.Operation.LedgerID {
			return empty, d.ErrInvestmentConflict
		}
	}
	l, err := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, m.Operation.LedgerID))
	if err != nil {
		return empty, err
	}
	if l.Archived {
		return empty, d.ErrInvestmentPrecondition
	}
	if l.Version != m.ExpectedLedgerVersion {
		return empty, d.ErrInvestmentConflict
	}
	ops, err := loadInvestmentOperations(ctx, tx, t, l.ID)
	if err != nil {
		return empty, err
	}
	if m.Action != "void" {
		if e := verifyOperationLotConversion(ctx, tx, t, m.Operation, true); e != nil {
			return empty, e
		}
	}
	if m.PreviewHash != "" {
		checked, e := r.previewMutationTx(ctx, tx, t, user, m)
		if e != nil {
			return empty, e
		}
		if !checked.Valid || checked.Hash != m.PreviewHash {
			return empty, d.ErrInvestmentConflict
		}
	}
	var sequence int64
	if m.Action == "create" {
		if err = tx.QueryRow(ctx, `UPDATE investment_ledgers SET next_sequence=next_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING next_sequence-1`, t, l.ID).Scan(&sequence); err != nil {
			return empty, err
		}
	}
	newID := uuid.NewString()
	if transfer && m.Action == "create" && m.Operation.ID != "" {
		newID = m.Operation.ID
	}
	proposed, ops, before, dirty, err := inv.ProposeMutation(l, ops, m, newID, sequence, time.Now().UTC().Truncate(time.Microsecond), transfer)
	if err != nil {
		return empty, err
	}
	var connected inv.PortfolioReplay
	var replay inv.ReplayResult
	if !deferReplay {
		connected, err = readConnectedInvestmentReplay(ctx, tx, t, []string{l.ID}, map[string]investmentReplayInput{l.ID: {l, ops}}, time.Now().UTC(), nil)
		if err != nil {
			return empty, err
		}
		replay = connected.Ledgers[l.ID]
	}
	raw, _ := json.Marshal(proposed)
	if m.Action == "create" {
		_, err = tx.Exec(ctx, `INSERT INTO investment_operations(id,tenant_id,ledger_id,kind,occurred_at,settlement_at,sequence,version,status,data,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, proposed.ID, t, l.ID, proposed.Kind, proposed.OccurredAt, proposed.SettlementAt, proposed.Sequence, proposed.Version, proposed.Status, raw, proposed.CreatedAt)
	} else {
		_, err = tx.Exec(ctx, `UPDATE investment_operations SET kind=$4,occurred_at=$5,settlement_at=$6,version=$7,status=$8,data=$9 WHERE tenant_id=$1 AND ledger_id=$2 AND id=$3`, t, l.ID, proposed.ID, proposed.Kind, proposed.OccurredAt, proposed.SettlementAt, proposed.Version, proposed.Status, raw)
	}
	if err != nil {
		return empty, err
	}
	var beforeRaw []byte
	if m.Source != nil {
		if _, err = tx.Exec(ctx, `INSERT INTO investment_import_ledger_bindings(tenant_id,import_id,account_id,ledger_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, t, m.Source.ImportID, sourceAccount, l.ID); err != nil {
			return empty, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO investment_source_links(tenant_id,import_id,record,source_version,account_id,ledger_id,operation_id,review_reason,actor_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, t, m.Source.ImportID, m.Source.Record, m.Source.ExpectedVersion, sourceAccount, l.ID, proposed.ID, m.Source.ReviewReason, user); err != nil {
			return empty, err
		}
	}
	if before != nil {
		beforeRaw, _ = json.Marshal(before)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO investment_operation_revisions(tenant_id,ledger_id,operation_id,version,action,before_data,after_data,actor_id,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, t, l.ID, proposed.ID, proposed.Version, m.Action, beforeRaw, raw, user, m.Reason); err != nil {
		return empty, err
	}
	if !deferReplay {
		if err = replaceInvestmentLegs(ctx, tx, t, l.ID, replay); err != nil {
			return empty, err
		}
		if err = replaceConnectedInvestmentDependents(ctx, tx, t, connected, map[string]bool{l.ID: true}, dirty); err != nil {
			return empty, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE investment_ledgers SET version=version+1 WHERE tenant_id=$1 AND id=$2`, t, l.ID); err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO investment_recalculation_jobs(tenant_id,ledger_id,journal_revision,dirty_from) VALUES($1,$2,$3,$4)`, t, l.ID, l.Version+1, dirty); err != nil {
		return empty, err
	}
	if err = saveInvestmentRequest(ctx, tx, t, "investment.operation.mutate", m.RequestKey, hash, proposed.ID); err != nil {
		return empty, err
	}
	return proposed, nil
}

func investmentNumeric(value string) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(value)
	return n
}
