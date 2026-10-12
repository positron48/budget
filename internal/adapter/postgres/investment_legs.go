package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

func replaceInvestmentLegs(ctx context.Context, tx pgx.Tx, tenant, ledger string, replay inv.ReplayResult) error {
	var err error
	if _, err = tx.Exec(ctx, `DELETE FROM investment_cash_legs WHERE tenant_id=$1 AND ledger_id=$2`, tenant, ledger); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM investment_security_legs WHERE tenant_id=$1 AND ledger_id=$2`, tenant, ledger); err != nil {
		return err
	}
	cash := make([][]any, 0, len(replay.CashLegs))
	for i, leg := range replay.CashLegs {
		cash = append(cash, []any{tenant, ledger, leg.OperationID, i, leg.Currency, leg.Bucket, leg.Component, investmentNumeric(leg.Amount), leg.EffectiveAt})
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"investment_cash_legs"}, []string{"tenant_id", "ledger_id", "operation_id", "ordinal", "currency", "bucket", "component", "amount", "effective_at"}, pgx.CopyFromRows(cash)); err != nil {
		return err
	}
	securities := make([][]any, 0, len(replay.SecurityLegs))
	for i, leg := range replay.SecurityLegs {
		securities = append(securities, []any{tenant, ledger, leg.OperationID, i, leg.InstrumentKey, investmentNumeric(leg.Quantity), leg.EffectiveAt})
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"investment_security_legs"}, []string{"tenant_id", "ledger_id", "operation_id", "ordinal", "instrument_key", "quantity", "effective_at"}, pgx.CopyFromRows(securities)); err != nil {
		return err
	}
	return nil
}
