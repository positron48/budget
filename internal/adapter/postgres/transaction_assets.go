package postgres

import (
	"context"
	"github.com/positron48/budget/internal/domain"
)

func (r *TransactionRepo) Create(ctx context.Context, t domain.Transaction) (domain.Transaction, error) {
	req := requestInContext(ctx)
	key := t.RequestKey
	var payload any = t
	if req.Key != "" {
		key = req.Key
		payload = req.Payload
	}
	return financialWrite(ctx, r.pool, t.TenantID, "create_transaction", key, payload, func(ctx context.Context) (domain.Transaction, error) {
		if err := r.validateAccount(ctx, t); err != nil {
			return t, err
		}
		created, err := r.create(ctx, t)
		if err != nil {
			return t, err
		}
		if err = r.syncMovement(ctx, created); err != nil {
			return t, err
		}
		return created, nil
	})
}
func (r *TransactionRepo) Update(ctx context.Context, t domain.Transaction) (domain.Transaction, error) {
	return financialWrite(ctx, r.pool, t.TenantID, "update_transaction", "", nil, func(ctx context.Context) (domain.Transaction, error) {
		old, err := r.get(ctx, t.ID, true)
		if err != nil {
			return t, err
		}
		if old.TenantID != t.TenantID {
			return t, domain.ErrAssetInvalid
		}
		if err = lockAssetAccounts(ctx, r.pool, t.TenantID, old.AssetAccountID, t.AssetAccountID); err != nil {
			return t, err
		}
		if err = r.validateAccount(ctx, t); err != nil {
			return t, err
		}
		updated, err := r.update(ctx, t)
		if err != nil {
			return t, err
		}
		if err = r.syncMovement(ctx, updated); err != nil {
			return t, err
		}
		return updated, nil
	})
}
func (r *TransactionRepo) Delete(ctx context.Context, id string) error {
	_, err := financialWrite(ctx, r.pool, activeTenant(ctx), "delete_transaction", "", nil, func(ctx context.Context) (bool, error) {
		old, e := r.get(ctx, id, true)
		if e != nil {
			return false, e
		}
		if e = lockAssetAccounts(ctx, r.pool, old.TenantID, old.AssetAccountID); e != nil {
			return false, e
		}
		_, e = r.pool.query(ctx).Exec(ctx, `DELETE FROM transactions WHERE id=$1 AND tenant_id=$2`, id, old.TenantID)
		return e == nil, e
	})
	return err
}
func (r *TransactionRepo) validateAccount(ctx context.Context, t domain.Transaction) error {
	if t.AssetAccountID == "" {
		return nil
	}
	if t.Amount.MinorUnits <= 0 || (t.Type != domain.TransactionTypeIncome && t.Type != domain.TransactionTypeExpense) {
		return domain.ErrAssetInvalid
	}
	_, err := validateAssetLeg(ctx, r.pool, t.TenantID, t.AssetAccountID, t.Amount.CurrencyCode, true)
	return err
}
func (r *TransactionRepo) syncMovement(ctx context.Context, t domain.Transaction) error {
	_, err := r.pool.query(ctx).Exec(ctx, `DELETE FROM asset_movements WHERE transaction_id=$1 AND tenant_id=$2`, t.ID, t.TenantID)
	if err != nil {
		return err
	}
	if t.AssetAccountID == "" {
		return nil
	}
	amount := t.Amount.MinorUnits
	if t.Type == domain.TransactionTypeExpense {
		amount = -amount
	}
	_, err = r.pool.query(ctx).Exec(ctx, `INSERT INTO asset_movements(tenant_id,account_id,currency_code,amount_minor,occurred_at,transaction_id,leg) VALUES($1,$2,$3,$4,$5,$6,'transaction')`, t.TenantID, t.AssetAccountID, t.Amount.CurrencyCode, amount, t.OccurredAt, t.ID)
	return err
}
