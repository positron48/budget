package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"time"
)

func scanCashTransfer(row pgx.Row) (inv.CashTransfer, error) {
	var v inv.CashTransfer
	var raw []byte
	if e := row.Scan(&raw); e != nil {
		return v, investmentError(e)
	}
	e := json.Unmarshal(raw, &v)
	return v, e
}
func (r *InvestmentRepo) GetCashTransfer(ctx context.Context, t, id string) (inv.CashTransfer, error) {
	return scanCashTransfer(r.pool.DB.QueryRow(ctx, `SELECT data FROM investment_cash_links WHERE tenant_id=$1 AND transfer_id=$2`, t, id))
}
func (r *InvestmentRepo) MutateCashTransfer(ctx context.Context, t, u string, m inv.CashTransferMutation) (inv.CashTransfer, error) {
	var zero inv.CashTransfer
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return zero, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = lockInvestment(ctx, tx, t); e != nil {
		return zero, e
	}
	if e = groupAccess(ctx, tx, t, u, false); e != nil {
		return zero, e
	}
	hashInput := m
	hashInput.RequestKey = ""
	hash := investmentHash(hashInput)
	// Budget integration is intentionally outside the portable journal requests.
	retry, e := investmentRetry(ctx, tx, t, "asset.investment.cash", m.RequestKey, hash)
	if e != nil {
		return zero, e
	}
	if retry != "" {
		v, e := scanCashTransfer(tx.QueryRow(ctx, `SELECT data FROM investment_cash_links WHERE tenant_id=$1 AND transfer_id=$2`, t, retry))
		if e != nil {
			return zero, e
		}
		return v, tx.Commit(ctx)
	}

	prepared, plan, e := r.prepareCashTransferTx(ctx, tx, t, u, m, true)
	if e != nil {
		return zero, e
	}
	if m.PreviewHash != "" {
		checked, err := r.cashTransferPreviewTx(ctx, tx, t, u, m, prepared, plan)
		if err != nil {
			return zero, err
		}
		if !checked.Preview.Valid || checked.Preview.Hash != m.PreviewHash {
			return zero, d.ErrInvestmentConflict
		}
	}
	v, ordinary, bankMoney, inbound, mutation := prepared.value, prepared.ordinary, prepared.bankMoney, prepared.inbound, plan.mutations[0]
	ctx = context.WithValue(ctx, financialTxKey{}, tx)

	saved, e := r.mutateInvestmentFactsTx(ctx, tx, t, u, mutation, true, false)
	if e != nil {
		return zero, e
	}
	v.OperationID = saved.ID
	v.Transfer.InvestmentCashTransfer = true
	if v.Transfer.Version != saved.Version {
		return zero, d.ErrInvestmentConflict
	}
	if m.Action == "create" {
		_, e = tx.Exec(ctx, `INSERT INTO asset_transfers(id,tenant_id,user_id,from_account_id,to_account_id,from_amount_minor,from_currency_code,to_amount_minor,to_currency_code,occurred_at,note,version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, v.Transfer.ID, t, u, v.Transfer.FromAccountID, v.Transfer.ToAccountID, v.Transfer.FromAmount.MinorUnits, v.Transfer.FromAmount.CurrencyCode, v.Transfer.ToAmount.MinorUnits, v.Transfer.ToAmount.CurrencyCode, v.Transfer.OccurredAt, v.Transfer.Note, v.Transfer.Version, v.Transfer.CreatedAt)
	} else {
		tag, err := tx.Exec(ctx, `UPDATE asset_transfers SET user_id=$3,from_amount_minor=$4,from_currency_code=$5,to_amount_minor=$6,to_currency_code=$7,occurred_at=$8,note=$9,version=$10 WHERE tenant_id=$1 AND id=$2 AND version=$11`, t, v.Transfer.ID, u, v.Transfer.FromAmount.MinorUnits, v.Transfer.FromAmount.CurrencyCode, v.Transfer.ToAmount.MinorUnits, v.Transfer.ToAmount.CurrencyCode, v.Transfer.OccurredAt, v.Transfer.Note, v.Transfer.Version, m.ExpectedVersion)
		e = err
		if e == nil && tag.RowsAffected() != 1 {
			return zero, d.ErrInvestmentConflict
		}
	}
	if e != nil {
		return zero, e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM asset_movements WHERE tenant_id=$1 AND transfer_id=$2`, t, v.Transfer.ID); e != nil {
		return zero, e
	}
	if v.Status == "posted" {
		signed := bankMoney.MinorUnits
		leg := "to"
		if inbound {
			signed = -signed
			leg = "from"
		}
		if _, e = tx.Exec(ctx, `INSERT INTO asset_movements(tenant_id,account_id,currency_code,amount_minor,occurred_at,transfer_id,leg) VALUES($1,$2,$3,$4,$5,$6,$7)`, t, ordinary.ID, bankMoney.CurrencyCode, signed, v.Transfer.OccurredAt, v.Transfer.ID, leg); e != nil {
			return zero, e
		}
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return zero, e
	}
	revision, e := json.Marshal(struct {
		Document                inv.CashTransfer
		Action, Reason, ActorID string
		RecordedAt              time.Time
	}{v, m.Action, m.Reason, u, time.Now().UTC().Truncate(time.Microsecond)})
	if e != nil {
		return zero, e
	}
	if m.Action == "create" {
		_, e = tx.Exec(ctx, `INSERT INTO investment_cash_links(tenant_id,transfer_id,ledger_id,operation_id,data,revisions) VALUES($1,$2,$3,$4,$5,jsonb_build_array($6::jsonb))`, t, v.Transfer.ID, v.LedgerID, v.OperationID, raw, revision)
	} else {
		_, e = tx.Exec(ctx, `UPDATE investment_cash_links SET data=$3,revisions=revisions||jsonb_build_array($4::jsonb) WHERE tenant_id=$1 AND transfer_id=$2`, t, v.Transfer.ID, raw, revision)
	}
	if e != nil {
		return zero, e
	}
	if e = saveInvestmentRequest(ctx, tx, t, "asset.investment.cash", m.RequestKey, hash, v.Transfer.ID); e != nil {
		return zero, e
	}
	return v, tx.Commit(ctx)
}

type cashTransferPlan struct {
	value, old inv.CashTransfer
	ordinary   d.AssetAccount
	from, to   d.AssetAccount
	bankMoney  d.Money
	inbound    bool
}

func (r *InvestmentRepo) prepareCashTransferTx(ctx context.Context, tx pgx.Tx, t, u string, m inv.CashTransferMutation, lock bool) (cashTransferPlan, linkedPreviewPlan, error) {
	var zero cashTransferPlan
	ePlan := linkedPreviewPlan{overrides: map[string]investmentReplayInput{}, next: map[string]int64{}}
	var e error
	ctx = context.WithValue(ctx, financialTxKey{}, tx)
	assets := NewAssetRepo(r.pool)
	if lock {
		if e = lockAssetAccounts(ctx, r.pool, t, m.Transfer.FromAccountID, m.Transfer.ToAccountID); e != nil {
			return zero, ePlan, e
		}
	}
	from, e := assets.getAccountRow(ctx, t, m.Transfer.FromAccountID, false)
	if e != nil {
		return zero, ePlan, investmentError(e)
	}
	to, e := assets.getAccountRow(ctx, t, m.Transfer.ToAccountID, false)
	if e != nil {
		return zero, ePlan, investmentError(e)
	}
	if from.Version != m.ExpectedFromAccountVersion || to.Version != m.ExpectedToAccountVersion {
		return zero, ePlan, d.ErrInvestmentConflict
	}
	// Exactly one calculated investment account, one ordinary initialized account.
	linked, ordinary := to, from
	inbound := true
	bankMoney, bookMoney := m.Transfer.FromAmount, m.Transfer.ToAmount
	if from.InvestmentMode == "ledger" {
		linked, ordinary = from, to
		inbound = false
		bankMoney, bookMoney = m.Transfer.ToAmount, m.Transfer.FromAmount
	}
	if linked.Archived || linked.InvestmentMode != "ledger" || ordinary.InvestmentMode == "ledger" || (ordinary.Kind != "bank" && ordinary.Kind != "cash" && ordinary.Kind != "deposit") {
		return zero, ePlan, d.ErrInvestmentPrecondition
	}
	v := inv.CashTransfer{Timing: m.Timing, Transfer: m.Transfer, LedgerID: linked.InvestmentLedgerID, Status: "posted"}
	var old inv.CashTransfer
	if m.Action == "create" {
		v.Transfer.ID = linkedPreviewID(t, u, m.RequestKey, "cash-transfer")
		v.Transfer.Version = 1
		v.Transfer.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	} else {
		old, e = scanCashTransfer(tx.QueryRow(ctx, `SELECT data FROM investment_cash_links WHERE tenant_id=$1 AND transfer_id=$2`, t, v.Transfer.ID))
		if e != nil {
			return zero, ePlan, e
		}
		if old.Status != "posted" || old.Transfer.Version != m.ExpectedVersion || old.Transfer.FromAccountID != v.Transfer.FromAccountID || old.Transfer.ToAccountID != v.Transfer.ToAccountID || old.LedgerID != v.LedgerID {
			return zero, ePlan, d.ErrInvestmentConflict
		}
		v.OperationID = old.OperationID
		v.Transfer.Version = old.Transfer.Version + 1
		v.Transfer.CreatedAt = old.Transfer.CreatedAt
		if m.Action == "void" {
			v = old
			v.Transfer.Version++
			v.Transfer.UserID = u
			v.Status = "voided"
			bankMoney, bookMoney = v.Transfer.FromAmount, v.Transfer.ToAmount
			if !inbound {
				bankMoney, bookMoney = bookMoney, bankMoney
			}
		}
	}
	if v.Transfer.OccurredAt.Before(linked.LedgerValuationFrom) {
		return zero, ePlan, d.ErrInvestmentPrecondition
	}
	if _, e = validateAssetLegWithLock(ctx, r.pool, t, ordinary.ID, bankMoney.CurrencyCode, true, lock); e != nil {
		return zero, ePlan, e
	}
	kind := "deposit"
	if !inbound {
		kind = "withdrawal"
	}
	operation := d.InvestmentOperation{Timing: v.Timing, ID: v.OperationID, LedgerID: v.LedgerID, TenantID: t, ActorID: u, Kind: kind, BudgetTransferID: v.Transfer.ID, Currency: bookMoney.CurrencyCode, Amount: fmt.Sprintf("%d.%02d", bookMoney.MinorUnits/100, bookMoney.MinorUnits%100), OccurredAt: v.Transfer.OccurredAt, Note: v.Transfer.Note}
	if e = inv.ValidateOperation(&operation); e != nil {
		return zero, ePlan, e
	}
	mutation := inv.Mutation{Action: m.Action, Operation: operation, ExpectedLedgerVersion: m.ExpectedLedgerVersion, ExpectedOperationVersion: m.ExpectedVersion, Reason: m.Reason, RequestKey: "cash:" + investmentHash([]string{v.Transfer.ID, m.RequestKey})}
	if m.Action != "create" {
		op, e := scanInvestmentOperation(tx.QueryRow(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2 AND id=$3`, t, v.LedgerID, v.OperationID))
		if e != nil {
			return zero, ePlan, e
		}
		if op.BudgetTransferID != v.Transfer.ID || op.Version != old.Transfer.Version {
			return zero, ePlan, d.ErrInvestmentConflict
		}
	}
	if m.Action == "create" {
		mutation.Operation.ID = linkedPreviewID(t, u, m.RequestKey, "cash-operation")
	}
	if e = ePlan.add(ctx, tx, t, mutation); e != nil {
		return zero, ePlan, e
	}
	v.OperationID = ePlan.mutations[0].Operation.ID
	v.Transfer.InvestmentCashTransfer = true
	return cashTransferPlan{value: v, old: old, ordinary: ordinary, from: from, to: to, bankMoney: bankMoney, inbound: inbound}, ePlan, nil
}
