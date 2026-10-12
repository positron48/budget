package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"time"
)

// BrokerTradeReplay checks a hypothetical additional operation in a consistent,
// read-only journal snapshot. It does not reserve a sequence or source identity.
func (r *InvestmentImportRepo) BrokerTradeReplay(ctx context.Context, tenant string, operation d.InvestmentOperation) (imp.BrokerReplayCheck, error) {
	var result imp.BrokerReplayCheck
	tx, err := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ledger, err := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, tenant, operation.LedgerID))
	if err != nil {
		return result, err
	}
	operations, err := loadInvestmentOperations(ctx, tx, tenant, ledger.ID)
	if err != nil {
		return result, err
	}
	var sequence int64
	if err = tx.QueryRow(ctx, `SELECT next_sequence FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, tenant, ledger.ID).Scan(&sequence); err != nil {
		return result, err
	}
	operation.ID = uuid.NewString()
	operation.TenantID = tenant
	operation.Status = "posted"
	operation.Version = 1
	operation.Sequence = sequence
	result.LedgerVersion = ledger.Version
	_, err = replayInvestmentLedgerTx(ctx, tx, tenant, ledger, append(operations, operation), time.Now().UTC())
	if err != nil {
		if !errors.Is(err, d.ErrInvestmentPrecondition) && !errors.Is(err, d.ErrInvestmentInvalid) && !errors.Is(err, d.ErrInvestmentLimit) {
			return result, err
		}
		result.Issue = err.Error()
	} else {
		result.Valid = true
	}
	return result, tx.Commit(ctx)
}
