package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

// LinkBrokerTrade only attaches evidence to an existing exact journal trade.
// It cannot create cash/security movements. Claims survive later journal edits.
func (r *InvestmentImportRepo) LinkBrokerTrade(ctx context.Context, tenant, actor string, link imp.BrokerTradeLink) (string, error) {
	if err := imp.ValidateBrokerTradeLink(link); err != nil {
		return "", err
	}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "broker-import:"+tenant); err != nil {
		return "", err
	}
	if err = lockInvestment(ctx, tx, tenant); err != nil {
		return "", err
	}
	id, err := r.linkBrokerTradeTx(ctx, tx, tenant, actor, link)
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func (r *InvestmentImportRepo) linkBrokerTradeTx(ctx context.Context, tx pgx.Tx, tenant, actor string, link imp.BrokerTradeLink) (string, error) {
	var err error
	var role string
	if err = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, tenant, actor).Scan(&role); errors.Is(err, pgx.ErrNoRows) || err == nil && role != "owner" && role != "admin" {
		return "", imp.ErrForbidden
	} else if err != nil {
		return "", err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT original_json FROM investment_broker_snapshots WHERE tenant_id=$1 AND id=$2`, tenant, link.SnapshotID).Scan(&raw); err != nil {
		return "", importError(err)
	}
	snapshot, err := imp.PrepareBrokerSnapshot(raw)
	if err != nil {
		return "", imp.ErrInvalid
	}
	candidate, err := imp.PrepareBrokerTrade(snapshot, link.BrokerOperationID, link.LedgerID)
	if err != nil {
		return "", imp.ErrInvalid
	}
	operation, err := scanInvestmentOperation(tx.QueryRow(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2 AND id=$3 FOR UPDATE`, tenant, link.LedgerID, link.OperationID))
	if err != nil {
		return "", importError(err)
	}
	if operation.Version != link.ExpectedOperationVersion {
		return "", imp.ErrConflict
	}
	matches := imp.PossibleBrokerTradeMatches(candidate.Operation, []d.InvestmentOperation{operation})
	if len(matches) != 1 || !matches[0].SameInstrument || !matches[0].SameLedger || !matches[0].ExactTime {
		return "", imp.ErrConflict
	}
	sources := map[string]imp.BrokerSourceRow{}
	for _, row := range snapshot.Rows {
		sources[row.Identity] = row
	}
	for _, identity := range candidate.SourceIdentities {
		row := sources[identity]
		var changed bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_broker_observations WHERE tenant_id=$1 AND account_id=$2 AND operation_id=$3 AND content_sha256<>$4)`, tenant, row.AccountID, row.OperationID, row.SHA256).Scan(&changed); err != nil {
			return "", err
		}
		if changed {
			return "", imp.ErrConflict
		}
	}
	// Identical retries return the persisted link. Different evidence or targets
	// cannot reuse either the journal operation or any claimed provider identity.
	var existing, snapshotID, reason string
	var version int64
	err = tx.QueryRow(ctx, `SELECT id,snapshot_id,operation_version,reason FROM investment_broker_links WHERE tenant_id=$1 AND ledger_id=$2 AND operation_id=$3`, tenant, link.LedgerID, link.OperationID).Scan(&existing, &snapshotID, &version, &reason)
	if err == nil {
		var count int
		primary := sources[candidate.SourceIdentities[0]]
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM investment_broker_claims WHERE tenant_id=$1 AND link_id=$2 AND account_id=$3 AND operation_id=$4 AND content_sha256=$5`, tenant, existing, primary.AccountID, primary.OperationID, primary.SHA256).Scan(&count); err != nil {
			return "", err
		}
		if snapshotID != link.SnapshotID || version != link.ExpectedOperationVersion || reason != link.Reason || count != 1 {
			return "", imp.ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	for _, identity := range candidate.SourceIdentities {
		row := sources[identity]
		var used bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_broker_claims WHERE tenant_id=$1 AND account_id=$2 AND operation_id=$3)`, tenant, row.AccountID, row.OperationID).Scan(&used); err != nil {
			return "", err
		}
		if used {
			return "", imp.ErrConflict
		}
	}
	id := uuid.NewString()
	if _, err = tx.Exec(ctx, `INSERT INTO investment_broker_links(id,tenant_id,snapshot_id,ledger_id,operation_id,operation_version,reason,actor_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, tenant, link.SnapshotID, link.LedgerID, link.OperationID, link.ExpectedOperationVersion, link.Reason, actor); err != nil {
		return "", err
	}
	for _, identity := range candidate.SourceIdentities {
		row := sources[identity]
		if _, err = tx.Exec(ctx, `INSERT INTO investment_broker_claims(tenant_id,account_id,operation_id,content_sha256,link_id) VALUES($1,$2,$3,$4,$5)`, tenant, row.AccountID, row.OperationID, row.SHA256, id); err != nil {
			return "", err
		}
	}
	return id, nil
}
