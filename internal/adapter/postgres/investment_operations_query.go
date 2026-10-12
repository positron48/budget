package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

func checkJournalReader(ctx context.Context, tx pgx.Tx, t, u string) error {
	var allowed bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 AND role IN ('owner','admin','member'))`, t, u).Scan(&allowed); e != nil {
		return e
	}
	if !allowed {
		return d.ErrInvestmentForbidden
	}
	return nil
}

const queryOperationHeadColumns = `o.data,o.id,o.tenant_id,o.ledger_id,o.version,o.status,o.kind,o.occurred_at,o.settlement_at,o.sequence`
const queryInvestmentOperationsSQL = `SELECT ` + queryOperationHeadColumns + `,COALESCE((SELECT b.operation_version FROM investment_broker_links b WHERE b.tenant_id=o.tenant_id AND b.ledger_id=o.ledger_id AND b.operation_id=o.id),0) FROM investment_operations o WHERE o.tenant_id=$1 AND (cardinality($2::uuid[])=0 OR o.ledger_id=ANY($2::uuid[])) ORDER BY o.occurred_at DESC,o.sequence DESC,o.id LIMIT $3`

func scanQueryOperation(row pgx.Row, brokerVersion *int64) (d.InvestmentOperation, error) {
	var o, head d.InvestmentOperation
	var raw []byte
	args := []any{&raw, &head.ID, &head.TenantID, &head.LedgerID, &head.Version, &head.Status, &head.Kind, &head.OccurredAt, &head.SettlementAt, &head.Sequence}
	if brokerVersion != nil {
		args = append(args, brokerVersion)
	}
	if e := row.Scan(args...); e != nil {
		return o, investmentError(e)
	}
	if e := json.Unmarshal(raw, &o); e != nil {
		return o, e
	}
	if o.ID != head.ID || o.TenantID != head.TenantID || o.LedgerID != head.LedgerID || o.Version != head.Version || o.Status != head.Status || o.Kind != head.Kind || o.Sequence != head.Sequence || !o.OccurredAt.Equal(head.OccurredAt) || !o.SettlementAt.Equal(head.SettlementAt) {
		return o, d.ErrInvestmentPrecondition
	}
	return o, nil
}

func (r *InvestmentRepo) QueryOperations(ctx context.Context, t, u string, q inv.OperationsQuery) (inv.OperationsPage, error) {
	var out inv.OperationsPage
	q, e := inv.NormalizeOperationsQuery(q)
	if e != nil {
		return out, e
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = checkJournalReader(ctx, tx, t, u); e != nil {
		return out, e
	}
	rows, e := tx.Query(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 ORDER BY id LIMIT 101`, t)
	if e != nil {
		return out, e
	}
	ledgers := []d.InvestmentLedger{}
	for rows.Next() {
		l, err := scanLedger(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		ledgers = append(ledgers, l)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(ledgers) > 100 {
		return out, d.ErrInvestmentLimit
	}
	rows, e = tx.Query(ctx, queryInvestmentOperationsSQL, t, q.LedgerIDs, inv.MaxJournalQueryOperations+1)
	if e != nil {
		return out, e
	}
	ops := []d.InvestmentOperation{}
	broker := map[string]int64{}
	for rows.Next() {
		var linked int64
		o, err := scanQueryOperation(rows, &linked)
		if err != nil {
			rows.Close()
			return out, err
		}
		ops = append(ops, o)
		broker[o.ID] = linked
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	out, e = inv.BuildOperationsPage(t, ledgers, ops, broker, q)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (r *InvestmentRepo) OperationHistory(ctx context.Context, t, u string, q inv.OperationHistoryQuery) (inv.OperationHistory, error) {
	var out inv.OperationHistory
	q, e := inv.NormalizeOperationHistoryQuery(q)
	if e != nil {
		return out, e
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = checkJournalReader(ctx, tx, t, u); e != nil {
		return out, e
	}
	out.Ledger, e = scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, t, q.LedgerID))
	if e != nil {
		return out, e
	}
	out.Current, e = scanQueryOperation(tx.QueryRow(ctx, `SELECT `+queryOperationHeadColumns+` FROM investment_operations o WHERE o.tenant_id=$1 AND o.ledger_id=$2 AND o.id=$3`, t, q.LedgerID, q.OperationID), nil)
	if e != nil {
		return out, e
	}
	if out.Current.ID != q.OperationID || out.Current.TenantID != t || out.Current.LedgerID != q.LedgerID || out.Current.Version < 1 || (out.Current.Status != "posted" && out.Current.Status != "voided") {
		return out, d.ErrInvestmentPrecondition
	}
	currentCopy := out.Current
	if e = inv.ValidateOperation(&currentCopy); e != nil {
		return out, d.ErrInvestmentPrecondition
	}
	if q.ExpectedVersion > 0 && out.Current.Version != q.ExpectedVersion {
		return out, d.ErrInvestmentConflict
	}
	if q.AfterVersion >= out.Current.Version {
		return out, d.ErrInvestmentInvalid
	}
	rows, e := tx.Query(ctx, `SELECT version,action,actor_id,reason,recorded_at,before_data,after_data FROM investment_operation_revisions WHERE tenant_id=$1 AND ledger_id=$2 AND operation_id=$3 AND version>$4 ORDER BY version LIMIT $5`, t, q.LedgerID, q.OperationID, q.AfterVersion, q.Limit+1)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	out.Revisions = []inv.OperationRevision{}
	for rows.Next() {
		var v inv.OperationRevision
		var before, after []byte
		if e = rows.Scan(&v.Version, &v.Action, &v.ActorID, &v.Reason, &v.RecordedAt, &before, &after); e != nil {
			return out, e
		}
		if len(before) > 0 && string(before) != "null" {
			var o d.InvestmentOperation
			if e = json.Unmarshal(before, &o); e != nil {
				return out, e
			}
			v.Before = &o
		}
		if e = json.Unmarshal(after, &v.After); e != nil {
			return out, e
		}
		if v.After.Version != v.Version || v.Before != nil && (v.Before.ID != q.OperationID || v.Before.TenantID != t || v.Before.LedgerID != q.LedgerID || v.Before.Version != v.Version-1) || v.After.ID != q.OperationID || v.After.TenantID != t || v.After.LedgerID != q.LedgerID {
			return out, d.ErrInvestmentPrecondition
		}
		out.Revisions = append(out.Revisions, v)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	rows.Close()
	if len(out.Revisions) == 0 {
		return out, d.ErrInvestmentPrecondition
	}
	if len(out.Revisions) > q.Limit {
		out.Revisions = out.Revisions[:q.Limit]
		out.NextAfterVersion = out.Revisions[len(out.Revisions)-1].Version
	}
	if q.AfterVersion > 0 {
		var previousRaw []byte
		if e = tx.QueryRow(ctx, `SELECT after_data FROM investment_operation_revisions WHERE tenant_id=$1 AND ledger_id=$2 AND operation_id=$3 AND version=$4`, t, q.LedgerID, q.OperationID, q.AfterVersion).Scan(&previousRaw); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return out, d.ErrInvestmentPrecondition
			}
			return out, e
		}
		var previous d.InvestmentOperation
		if json.Unmarshal(previousRaw, &previous) != nil || !reflect.DeepEqual(out.Revisions[0].Before, &previous) {
			return out, d.ErrInvestmentPrecondition
		}
	}
	if out.Revisions[0].Version != q.AfterVersion+1 {
		return out, d.ErrInvestmentPrecondition
	}
	for i, v := range out.Revisions {
		if i > 0 && v.Version != out.Revisions[i-1].Version+1 {
			return out, d.ErrInvestmentPrecondition
		}
	}
	if out.NextAfterVersion == 0 && out.Revisions[len(out.Revisions)-1].Version != out.Current.Version {
		return out, d.ErrInvestmentPrecondition
	}
	if out.NextAfterVersion == 0 && !reflect.DeepEqual(out.Revisions[len(out.Revisions)-1].After, out.Current) {
		return out, d.ErrInvestmentPrecondition
	}
	for i, v := range out.Revisions {
		if v.Version == 1 {
			if v.Action != "create" || v.Before != nil || v.After.Status != "posted" {
				return out, d.ErrInvestmentPrecondition
			}
		} else if v.Before == nil || v.Before.Status != "posted" || (v.Action != "amend" && v.Action != "void") || v.Action == "amend" && v.After.Status != "posted" || v.Action == "void" && v.After.Status != "voided" {
			return out, d.ErrInvestmentPrecondition
		}
		if i > 0 && !reflect.DeepEqual(v.Before, &out.Revisions[i-1].After) {
			return out, d.ErrInvestmentPrecondition
		}
	}
	return out, tx.Commit(ctx)
}
