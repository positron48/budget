package postgres

import (
	"context"
	"github.com/positron48/budget/internal/domain"
	"sort"
)

type CurrencyExchangeRepo struct{ pool *Pool }

func NewCurrencyExchangeRepo(pool *Pool) *CurrencyExchangeRepo { return &CurrencyExchangeRepo{pool} }

const exchangeColumns = `id,tenant_id,user_id,from_amount_numeric::text,from_currency_code,to_amount_numeric::text,to_currency_code,rate_numeric::text,occurred_at,note,created_at,COALESCE(from_asset_account_id::text,''),COALESCE(to_asset_account_id::text,'')`

func scanExchange(row interface{ Scan(...any) error }) (domain.CurrencyExchange, error) {
	var e domain.CurrencyExchange
	var from, to string
	err := row.Scan(&e.ID, &e.TenantID, &e.UserID, &from, &e.FromAmount.CurrencyCode, &to, &e.ToAmount.CurrencyCode, &e.RateDecimal, &e.OccurredAt, &e.Note, &e.CreatedAt, &e.FromAssetAccountID, &e.ToAssetAccountID)
	e.FromAmount.MinorUnits = fromDecimal(from)
	e.ToAmount.MinorUnits = fromDecimal(to)
	return e, err
}
func (r *CurrencyExchangeRepo) get(ctx context.Context, tenant, id string, lock bool) (domain.CurrencyExchange, error) {
	return scanExchange(r.pool.query(ctx).QueryRow(ctx, `SELECT `+exchangeColumns+` FROM currency_exchanges WHERE tenant_id=$1 AND id=$2`+lockClause(lock), tenant, id))
}
func (r *CurrencyExchangeRepo) validateAccounts(ctx context.Context, e domain.CurrencyExchange) error {
	if (e.FromAssetAccountID == "") != (e.ToAssetAccountID == "") {
		return domain.ErrAssetInvalid
	}
	if e.FromAssetAccountID == "" {
		return nil
	}
	legs := []struct{ id, currency string }{{e.FromAssetAccountID, e.FromAmount.CurrencyCode}, {e.ToAssetAccountID, e.ToAmount.CurrencyCode}}
	sort.Slice(legs, func(i, j int) bool { return legs[i].id < legs[j].id })
	for _, l := range legs {
		if _, err := validateAssetLeg(ctx, r.pool, e.TenantID, l.id, l.currency, true); err != nil {
			return err
		}
	}
	return nil
}
func (r *CurrencyExchangeRepo) syncMovements(ctx context.Context, e domain.CurrencyExchange) error {
	_, err := r.pool.query(ctx).Exec(ctx, `DELETE FROM asset_movements WHERE tenant_id=$1 AND exchange_id=$2`, e.TenantID, e.ID)
	if err != nil {
		return err
	}
	if e.FromAssetAccountID == "" {
		return nil
	}
	for _, l := range []struct {
		id    string
		money domain.Money
		sign  int64
		leg   string
	}{{e.FromAssetAccountID, e.FromAmount, -1, "from"}, {e.ToAssetAccountID, e.ToAmount, 1, "to"}} {
		_, err = r.pool.query(ctx).Exec(ctx, `INSERT INTO asset_movements(tenant_id,account_id,currency_code,amount_minor,occurred_at,exchange_id,leg) VALUES($1,$2,$3,$4,$5,$6,$7)`, e.TenantID, l.id, l.money.CurrencyCode, l.money.MinorUnits*l.sign, e.OccurredAt, e.ID, l.leg)
		if err != nil {
			return err
		}
	}
	return nil
}
func (r *CurrencyExchangeRepo) Create(ctx context.Context, e domain.CurrencyExchange) (domain.CurrencyExchange, error) {
	req := requestInContext(ctx)
	key := e.RequestKey
	var payload any = e
	if req.Key != "" {
		key = req.Key
		payload = req.Payload
	}
	return financialWrite(ctx, r.pool, e.TenantID, "create_exchange", key, payload, func(ctx context.Context) (domain.CurrencyExchange, error) {
		if err := r.validateAccounts(ctx, e); err != nil {
			return e, err
		}
		created, err := scanExchange(r.pool.query(ctx).QueryRow(ctx, `INSERT INTO currency_exchanges(tenant_id,user_id,from_amount_numeric,from_currency_code,to_amount_numeric,to_currency_code,occurred_at,note,from_asset_account_id,to_asset_account_id)
            VALUES($1,$2,$3::numeric,$4,$5::numeric,$6,$7,$8,NULLIF($9,'')::uuid,NULLIF($10,'')::uuid) RETURNING `+exchangeColumns, e.TenantID, e.UserID, toDecimal(e.FromAmount.MinorUnits), e.FromAmount.CurrencyCode, toDecimal(e.ToAmount.MinorUnits), e.ToAmount.CurrencyCode, e.OccurredAt, e.Note, e.FromAssetAccountID, e.ToAssetAccountID))
		if err != nil {
			return e, err
		}
		if err = r.syncMovements(ctx, created); err != nil {
			return e, err
		}
		return created, nil
	})
}
func (r *CurrencyExchangeRepo) Update(ctx context.Context, e domain.CurrencyExchange) (domain.CurrencyExchange, error) {
	return financialWrite(ctx, r.pool, e.TenantID, "update_exchange", "", nil, func(ctx context.Context) (domain.CurrencyExchange, error) {
		old, err := r.get(ctx, e.TenantID, e.ID, true)
		if err != nil {
			return e, err
		}
		if e.PreserveAssetAccounts {
			e.FromAssetAccountID = old.FromAssetAccountID
			e.ToAssetAccountID = old.ToAssetAccountID
		}
		if err = lockAssetAccounts(ctx, r.pool, e.TenantID, old.FromAssetAccountID, old.ToAssetAccountID, e.FromAssetAccountID, e.ToAssetAccountID); err != nil {
			return e, err
		}
		if err = r.validateAccounts(ctx, e); err != nil {
			return e, err
		}
		updated, err := scanExchange(r.pool.query(ctx).QueryRow(ctx, `UPDATE currency_exchanges SET from_amount_numeric=$3::numeric,from_currency_code=$4,to_amount_numeric=$5::numeric,to_currency_code=$6,occurred_at=$7,note=$8,from_asset_account_id=NULLIF($9,'')::uuid,to_asset_account_id=NULLIF($10,'')::uuid WHERE tenant_id=$1 AND id=$2 RETURNING `+exchangeColumns, e.TenantID, e.ID, toDecimal(e.FromAmount.MinorUnits), e.FromAmount.CurrencyCode, toDecimal(e.ToAmount.MinorUnits), e.ToAmount.CurrencyCode, e.OccurredAt, e.Note, e.FromAssetAccountID, e.ToAssetAccountID))
		if err != nil {
			return e, err
		}
		if err = r.syncMovements(ctx, updated); err != nil {
			return e, err
		}
		return updated, nil
	})
}
func (r *CurrencyExchangeRepo) List(ctx context.Context, tenant string, page, size int) ([]domain.CurrencyExchange, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	var total int64
	if err := r.pool.query(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM currency_exchanges WHERE tenant_id=$1`, tenant).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.query(ctx).Query(ctx, `SELECT `+exchangeColumns+` FROM currency_exchanges WHERE tenant_id=$1 ORDER BY occurred_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.CurrencyExchange{}
	for rows.Next() {
		e, err := scanExchange(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
func (r *CurrencyExchangeRepo) Delete(ctx context.Context, tenant, id string) error {
	_, err := financialWrite(ctx, r.pool, tenant, "delete_exchange", "", nil, func(ctx context.Context) (bool, error) {
		e, err := r.get(ctx, tenant, id, true)
		if err != nil {
			return false, err
		}
		if err = lockAssetAccounts(ctx, r.pool, tenant, e.FromAssetAccountID, e.ToAssetAccountID); err != nil {
			return false, err
		}
		_, err = r.pool.query(ctx).Exec(ctx, `DELETE FROM currency_exchanges WHERE tenant_id=$1 AND id=$2`, tenant, id)
		return err == nil, err
	})
	return err
}
