package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	inv "github.com/positron48/budget/internal/usecase/investment"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"time"
)

func (r *InvestmentImportRepo) PostBrokerTrade(ctx context.Context, tenant, actor string, p imp.BrokerTradePost) (imp.BrokerTradePostResult, error) {
	var empty imp.BrokerTradePostResult
	if err := imp.ValidateBrokerTradePost(p); err != nil {
		return empty, err
	}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "broker-import:"+tenant); err != nil {
		return empty, err
	}
	if err = lockInvestment(ctx, tx, tenant); err != nil {
		return empty, err
	}
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, tenant, actor).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && role != "owner" && role != "admin" {
		return empty, imp.ErrForbidden
	}
	if err != nil {
		return empty, err
	}
	hash := investmentHash(struct {
		Actor string
		Post  imp.BrokerTradePost
	}{actor, p})
	var savedHash string
	var saved []byte
	err = tx.QueryRow(ctx, `SELECT payload_hash,result FROM financial_write_requests WHERE tenant_id=$1 AND operation='investment.broker.post' AND request_key=$2`, tenant, p.RequestKey).Scan(&savedHash, &saved)
	if err == nil {
		if savedHash != hash {
			return empty, imp.ErrConflict
		}
		if err = json.Unmarshal(saved, &empty); err != nil {
			return empty, err
		}
		return empty, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT original_json FROM investment_broker_snapshots WHERE tenant_id=$1 AND id=$2`, tenant, p.SnapshotID).Scan(&raw); err != nil {
		return empty, importError(err)
	}
	snapshot, err := imp.PrepareBrokerSnapshot(raw)
	if err != nil {
		return empty, imp.ErrInvalid
	}
	candidate, err := imp.PrepareBrokerTrade(snapshot, p.BrokerOperationID, p.LedgerID)
	if err != nil {
		return empty, imp.ErrInvalid
	}
	operation := candidate.Operation
	if operation.OccurredAt.After(time.Now().UTC().Add(5 * time.Minute)) {
		return empty, imp.ErrInvalid
	}
	// The investment lock serializes this query with all journal mutations.
	candidates, err := brokerDistinctCandidates(ctx, tx, tenant, operation.OccurredAt, candidate.SourceIdentities)
	if err != nil {
		return empty, err
	}
	if len(imp.PossibleBrokerTradeMatches(operation, candidates)) > 0 {
		return empty, imp.ErrConflict
	}
	operation.TenantID = tenant
	operation.ActorID = actor
	journal := NewInvestmentRepo(r.pool)
	posted, err := journal.mutateInvestmentTx(ctx, tx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "broker:" + uuid.NewString(), Reason: p.Reason, ExpectedLedgerVersion: p.ExpectedLedgerVersion, Operation: operation}, false)
	if err != nil {
		return empty, err
	}
	linkID, err := r.linkBrokerTradeTx(ctx, tx, tenant, actor, imp.BrokerTradeLink{SnapshotID: p.SnapshotID, BrokerOperationID: p.BrokerOperationID, LedgerID: p.LedgerID, OperationID: posted.ID, ExpectedOperationVersion: posted.Version, Reason: p.Reason})
	if err != nil {
		return empty, err
	}
	result := imp.BrokerTradePostResult{OperationID: posted.ID, LinkID: linkID}
	encoded, err := json.Marshal(result)
	if err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO financial_write_requests(tenant_id,operation,request_key,payload_hash,result) VALUES($1,'investment.broker.post',$2,$3,$4)`, tenant, p.RequestKey, hash, encoded); err != nil {
		return empty, err
	}
	return result, tx.Commit(ctx)
}
