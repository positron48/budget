package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

func scanInvestmentTransfer(row pgx.Row) (d.InvestmentTransfer, error) {
	var raw []byte
	var v d.InvestmentTransfer
	if err := row.Scan(&raw); err != nil {
		return v, investmentError(err)
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}
func (r *InvestmentRepo) GetTransfer(ctx context.Context, t, id string) (d.InvestmentTransfer, error) {
	return scanInvestmentTransfer(r.pool.DB.QueryRow(ctx, `SELECT data FROM investment_transfers WHERE tenant_id=$1 AND id=$2`, t, id))
}
func (r *InvestmentRepo) MutateTransfer(ctx context.Context, t, user string, m inv.TransferMutation) (d.InvestmentTransfer, error) {
	empty := d.InvestmentTransfer{}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The same tenant lock serializes single- and multi-ledger financial writes.
	if err = lockInvestment(ctx, tx, t); err != nil {
		return empty, err
	}
	hashInput := m
	hashInput.RequestKey = ""
	hash := investmentHash(hashInput)
	retry, err := investmentRetry(ctx, tx, t, "investment.transfer.mutate", m.RequestKey, hash)
	if err != nil {
		return empty, err
	}
	if retry != "" {
		return scanInvestmentTransfer(tx.QueryRow(ctx, `SELECT data FROM investment_transfers WHERE tenant_id=$1 AND id=$2`, t, retry))
	}

	proposed, plan, err := r.prepareTransferTx(ctx, tx, t, user, m)
	if err != nil {
		return empty, err
	}
	if m.PreviewHash != "" {
		checked, e := r.transferPreviewTx(ctx, tx, t, user, m, proposed, plan)
		if e != nil {
			return empty, e
		}
		if !checked.Preview.Valid || checked.Preview.Hash != m.PreviewHash {
			return empty, d.ErrInvestmentConflict
		}
	}
	for _, mutation := range plan.mutations {
		if _, err = r.mutateInvestmentFactsTx(ctx, tx, t, user, mutation, true, true); err != nil {
			return empty, err
		}
	}
	dirty := plan.dirty

	connected, err := readConnectedInvestmentReplay(ctx, tx, t, []string{proposed.FromLedgerID, proposed.ToLedgerID}, nil, time.Now().UTC(), nil)
	if err != nil {
		return empty, err
	}
	if err = replaceConnectedInvestmentDependents(ctx, tx, t, connected, nil, dirty); err != nil {
		return empty, err
	}
	raw, _ := json.Marshal(proposed)
	if m.Action == "create" {
		_, err = tx.Exec(ctx, `INSERT INTO investment_transfers(tenant_id,id,from_ledger_id,to_ledger_id,from_operation_id,to_operation_id,data) VALUES($1,$2,$3,$4,$5,$6,$7)`, t, proposed.ID, proposed.FromLedgerID, proposed.ToLedgerID, proposed.FromOperationID, proposed.ToOperationID, raw)
	} else {
		_, err = tx.Exec(ctx, `UPDATE investment_transfers SET data=$3 WHERE tenant_id=$1 AND id=$2`, t, proposed.ID, raw)
	}
	if err != nil {
		return empty, err
	}
	if err = saveInvestmentRequest(ctx, tx, t, "investment.transfer.mutate", m.RequestKey, hash, proposed.ID); err != nil {
		return empty, err
	}
	return proposed, tx.Commit(ctx)
}

func (r *InvestmentRepo) prepareTransferTx(ctx context.Context, tx pgx.Tx, t, user string, m inv.TransferMutation) (d.InvestmentTransfer, linkedPreviewPlan, error) {
	empty := d.InvestmentTransfer{}
	plan := linkedPreviewPlan{overrides: map[string]investmentReplayInput{}, next: map[string]int64{}}
	var err error
	proposed := m.Transfer
	dirty := proposed.OccurredAt
	if m.Action == "create" {
		proposed.ID = linkedPreviewID(t, user, m.RequestKey, "transfer")
		proposed.Version = 1
		proposed.Status = "posted"
	} else {
		old, err := scanInvestmentTransfer(tx.QueryRow(ctx, `SELECT data FROM investment_transfers WHERE tenant_id=$1 AND id=$2`, t, proposed.ID))
		if err != nil {
			return empty, plan, err
		}
		if old.Version != m.ExpectedVersion || old.Status != "posted" || old.FromLedgerID != proposed.FromLedgerID || old.ToLedgerID != proposed.ToLedgerID {
			return empty, plan, d.ErrInvestmentConflict
		}
		if old.OccurredAt.Before(dirty) || dirty.IsZero() {
			dirty = old.OccurredAt
		}
		if m.Action == "amend" && (old.InstrumentKey == "") != (proposed.InstrumentKey == "") {
			return empty, plan, d.ErrInvestmentPrecondition
		}
		if m.Action == "void" {
			proposed = old
			proposed.Status = "voided"
		} else {
			proposed.FromOperationID = old.FromOperationID
			proposed.ToOperationID = old.ToOperationID
			proposed.Status = "posted"
		}
		proposed.Version = old.Version + 1
	}
	outKind, inKind := "transfer_out", "transfer_in"
	if proposed.InstrumentKey != "" {
		outKind, inKind = "security_transfer_out", "security_transfer_in"
	}
	for _, side := range []struct {
		ledger, other, kind, operation string
		version                        int64
	}{
		{proposed.FromLedgerID, proposed.ToLedgerID, outKind, proposed.FromOperationID, m.ExpectedFromVersion},
		{proposed.ToLedgerID, proposed.FromLedgerID, inKind, proposed.ToOperationID, m.ExpectedToVersion},
	} {
		operation := d.InvestmentOperation{Timing: proposed.Timing, ID: side.operation, TenantID: t, ActorID: user, LedgerID: side.ledger, CounterpartyLedgerID: side.other, TransferID: proposed.ID, Kind: side.kind, InstrumentKey: proposed.InstrumentKey, Quantity: proposed.Quantity, Currency: proposed.Currency, Amount: proposed.Amount, OccurredAt: proposed.OccurredAt, Note: proposed.Note}
		if err = inv.ValidateOperation(&operation); err != nil {
			return empty, plan, err
		}
		mutation := inv.Mutation{Action: m.Action, Operation: operation, Reason: m.Reason, RequestKey: "transfer:" + investmentHash([]string{proposed.ID, side.kind, m.RequestKey}), ExpectedLedgerVersion: side.version}
		if m.Action != "create" {
			old, err := scanInvestmentOperation(tx.QueryRow(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2 AND id=$3`, t, side.ledger, side.operation))
			if err != nil {
				return empty, plan, err
			}
			if old.TransferID != proposed.ID {
				return empty, plan, d.ErrInvestmentConflict
			}
			mutation.ExpectedOperationVersion = old.Version
		}
		if m.Action == "create" {
			mutation.Operation.ID = linkedPreviewID(t, user, m.RequestKey, side.kind)
		}
		if err = plan.add(ctx, tx, t, mutation); err != nil {
			return empty, plan, err
		}
		if side.kind == outKind {
			proposed.FromOperationID = plan.mutations[len(plan.mutations)-1].Operation.ID
		} else {
			proposed.ToOperationID = plan.mutations[len(plan.mutations)-1].Operation.ID
		}
	}
	plan.dirty = dirty
	return proposed, plan, nil
}
