package postgres

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/positron48/budget/internal/domain"
)

type AssetRepo struct{ pool *Pool }

func NewAssetRepo(pool *Pool) *AssetRepo { return &AssetRepo{pool} }

const accountColumns = `id, tenant_id, name, kind, institution, note, COALESCE(fixed_currency_code,''), COALESCE(deposit_rate::text,''), COALESCE(deposit_opened_on::text,''), COALESCE(deposit_matures_on::text,''), archived, version, created_at`

func scanAccount(row interface{ Scan(...any) error }) (domain.AssetAccount, error) {
	var a domain.AssetAccount
	err := row.Scan(&a.ID, &a.TenantID, &a.Name, &a.Kind, &a.Institution, &a.Note, &a.FixedCurrencyCode, &a.DepositRateDecimal, &a.DepositOpenedOn, &a.DepositMaturesOn, &a.Archived, &a.Version, &a.CreatedAt)
	a.Balances = []domain.AssetBalance{}
	return a, err
}
func (r *AssetRepo) getAccountRow(ctx context.Context, tenant, id string, lock bool) (domain.AssetAccount, error) {
	sql := `SELECT ` + accountColumns + ` FROM asset_accounts WHERE tenant_id=$1 AND id=$2`
	if lock {
		sql += ` FOR UPDATE`
	}
	return scanAccount(r.pool.query(ctx).QueryRow(ctx, sql, tenant, id))
}
func (r *AssetRepo) balances(ctx context.Context, tenant, id string, at time.Time) (map[string][]domain.AssetBalance, error) {
	rows, err := r.pool.query(ctx).Query(ctx, `
        WITH latest AS (
            SELECT DISTINCT ON (account_id,currency_code) account_id,currency_code,amount_minor,as_of
            FROM asset_snapshots WHERE tenant_id=$1 AND ($2='' OR account_id::text=$2) AND as_of <= $3
            ORDER BY account_id,currency_code,as_of DESC
        )
        SELECT s.account_id,s.currency_code,s.amount_minor,s.as_of,
            (s.amount_minor::numeric + COALESCE((SELECT SUM(m.amount_minor::numeric) FROM asset_movements m
             WHERE m.tenant_id=$1 AND m.account_id=s.account_id AND m.currency_code=s.currency_code
             AND m.occurred_at>s.as_of AND m.occurred_at<=$3),0))::text
        FROM latest s ORDER BY s.account_id,s.currency_code`, tenant, id, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]domain.AssetBalance{}
	for rows.Next() {
		var accountID, current string
		var b domain.AssetBalance
		if err = rows.Scan(&accountID, &b.Amount.CurrencyCode, &b.ConfirmedAmount.MinorUnits, &b.ConfirmedAt, &current); err != nil {
			return nil, err
		}
		b.ConfirmedAmount.CurrencyCode = b.Amount.CurrencyCode
		// Parse an exact integer; never round or silently wrap a balance overflow.
		if b.Amount.MinorUnits, err = parseMinorInteger(current); err != nil {
			return nil, err
		}
		out[accountID] = append(out[accountID], b)
	}
	return out, rows.Err()
}
func (r *AssetRepo) GetAccount(ctx context.Context, tenant, id string, at time.Time) (domain.AssetAccount, error) {
	a, err := r.getAccountRow(ctx, tenant, id, false)
	if err != nil {
		return a, err
	}
	balances, err := r.balances(ctx, tenant, id, at)
	if err != nil {
		return a, err
	}
	if b := balances[id]; b != nil {
		a.Balances = b
	}
	return a, nil
}
func (r *AssetRepo) ListAccounts(ctx context.Context, tenant string, archived bool, at time.Time) ([]domain.AssetAccount, error) {
	rows, err := r.pool.query(ctx).Query(ctx, `SELECT `+accountColumns+` FROM asset_accounts WHERE tenant_id=$1 AND ($2 OR NOT archived) ORDER BY archived,kind,institution,name,id`, tenant, archived)
	if err != nil {
		return nil, err
	}
	out := []domain.AssetAccount{}
	for rows.Next() {
		a, e := scanAccount(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	balances, err := r.balances(ctx, tenant, "", at)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if b := balances[out[i].ID]; b != nil {
			out[i].Balances = b
		}
	}
	return out, nil
}
func (r *AssetRepo) CreateAccount(ctx context.Context, a domain.AssetAccount, openings []domain.AssetSnapshot, key string) (domain.AssetAccount, error) {
	return financialWrite(ctx, r.pool, a.TenantID, "create_account", key, struct {
		Account  domain.AssetAccount
		Openings []domain.AssetSnapshot
	}{a, openings}, func(ctx context.Context) (domain.AssetAccount, error) {
		err := r.pool.query(ctx).QueryRow(ctx, `INSERT INTO asset_accounts(tenant_id,name,kind,institution,note,fixed_currency_code,deposit_rate,deposit_opened_on,deposit_matures_on)
            VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,'')::numeric,NULLIF($8,'')::date,NULLIF($9,'')::date) RETURNING id`, a.TenantID, a.Name, a.Kind, a.Institution, a.Note, a.FixedCurrencyCode, a.DepositRateDecimal, a.DepositOpenedOn, a.DepositMaturesOn).Scan(&a.ID)
		if err != nil {
			return domain.AssetAccount{}, err
		}
		for _, s := range openings {
			s.AccountID = a.ID
			s.TenantID = a.TenantID
			if err = checkSnapshotAccount(a, s); err != nil {
				return domain.AssetAccount{}, err
			}
			if _, err = r.insertSnapshot(ctx, s); err != nil {
				return domain.AssetAccount{}, err
			}
		}
		return r.GetAccount(ctx, a.TenantID, a.ID, time.Now())
	})
}
func (r *AssetRepo) UpdateAccount(ctx context.Context, a domain.AssetAccount) (domain.AssetAccount, error) {
	return financialWrite(ctx, r.pool, a.TenantID, "update_account", "", nil, func(ctx context.Context) (domain.AssetAccount, error) {
		old, err := r.getAccountRow(ctx, a.TenantID, a.ID, true)
		if err != nil {
			return old, err
		}
		if old.Version != a.Version {
			return old, domain.ErrAssetConflict
		}
		if old.Kind != a.Kind || old.FixedCurrencyCode != a.FixedCurrencyCode {
			return old, domain.ErrAssetInvalid
		}
		_, err = r.pool.query(ctx).Exec(ctx, `UPDATE asset_accounts SET name=$3,institution=$4,note=$5,deposit_rate=NULLIF($6,'')::numeric,deposit_opened_on=NULLIF($7,'')::date,deposit_matures_on=NULLIF($8,'')::date,version=version+1 WHERE tenant_id=$1 AND id=$2`, a.TenantID, a.ID, a.Name, a.Institution, a.Note, a.DepositRateDecimal, a.DepositOpenedOn, a.DepositMaturesOn)
		if err != nil {
			return old, err
		}
		return r.GetAccount(ctx, a.TenantID, a.ID, time.Now())
	})
}
func (r *AssetRepo) SetArchived(ctx context.Context, tenant, id string, version int64, value bool) (domain.AssetAccount, error) {
	return financialWrite(ctx, r.pool, tenant, "archive_account", "", nil, func(ctx context.Context) (domain.AssetAccount, error) {
		a, err := r.getAccountRow(ctx, tenant, id, true)
		if err != nil {
			return a, err
		}
		if a.Version != version {
			return a, domain.ErrAssetConflict
		}
		if value {
			b, e := r.balances(ctx, tenant, id, time.Now())
			if e != nil {
				return a, e
			}
			for _, v := range b[id] {
				if a.Kind != "property" && v.Amount.MinorUnits != 0 {
					return a, domain.ErrAssetPrecondition
				}
			}
			var future bool
			err = r.pool.query(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM asset_movements WHERE tenant_id=$1 AND account_id=$2 AND occurred_at>now()) OR EXISTS(SELECT 1 FROM asset_snapshots WHERE tenant_id=$1 AND account_id=$2 AND as_of>now())`, tenant, id).Scan(&future)
			if err != nil {
				return a, err
			}
			if future {
				return a, domain.ErrAssetPrecondition
			}
		}
		_, err = r.pool.query(ctx).Exec(ctx, `UPDATE asset_accounts SET archived=$3,version=version+1 WHERE tenant_id=$1 AND id=$2`, tenant, id, value)
		if err != nil {
			return a, err
		}
		return r.GetAccount(ctx, tenant, id, time.Now())
	})
}
func (r *AssetRepo) DeleteEmptyAccount(ctx context.Context, tenant, id string, version int64) error {
	_, err := financialWrite(ctx, r.pool, tenant, "delete_account", "", nil, func(ctx context.Context) (bool, error) {
		a, e := r.getAccountRow(ctx, tenant, id, true)
		if e != nil {
			return false, e
		}
		if a.Version != version {
			return false, domain.ErrAssetConflict
		}
		var used bool
		e = r.pool.query(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM asset_snapshots WHERE tenant_id=$1 AND account_id=$2) OR EXISTS(SELECT 1 FROM asset_movements WHERE tenant_id=$1 AND account_id=$2)`, tenant, id).Scan(&used)
		if e != nil {
			return false, e
		}
		if used {
			return false, domain.ErrAssetPrecondition
		}
		_, e = r.pool.query(ctx).Exec(ctx, `DELETE FROM asset_accounts WHERE tenant_id=$1 AND id=$2`, tenant, id)
		return e == nil, e
	})
	return err
}

const snapshotColumns = `id,tenant_id,account_id,COALESCE(user_id::text,''),currency_code,amount_minor,as_of,kind,note,version,created_at`

func scanSnapshot(row interface{ Scan(...any) error }) (domain.AssetSnapshot, error) {
	var s domain.AssetSnapshot
	err := row.Scan(&s.ID, &s.TenantID, &s.AccountID, &s.UserID, &s.Amount.CurrencyCode, &s.Amount.MinorUnits, &s.AsOf, &s.Kind, &s.Note, &s.Version, &s.CreatedAt)
	return s, err
}
func (r *AssetRepo) insertSnapshot(ctx context.Context, s domain.AssetSnapshot) (domain.AssetSnapshot, error) {
	return scanSnapshot(r.pool.query(ctx).QueryRow(ctx, `INSERT INTO asset_snapshots(tenant_id,account_id,user_id,currency_code,amount_minor,as_of,kind,note)
        VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8) RETURNING `+snapshotColumns, s.TenantID, s.AccountID, s.UserID, s.Amount.CurrencyCode, s.Amount.MinorUnits, s.AsOf, s.Kind, s.Note))
}
func checkSnapshotAccount(a domain.AssetAccount, s domain.AssetSnapshot) error {
	if a.Archived {
		return domain.ErrAssetPrecondition
	}
	if a.FixedCurrencyCode != "" && a.FixedCurrencyCode != s.Amount.CurrencyCode {
		return domain.ErrAssetInvalid
	}
	if s.Kind == "valuation" && a.Kind != "investment" && a.Kind != "property" {
		return domain.ErrAssetInvalid
	}
	if (a.Kind == "investment" || a.Kind == "property") && s.Kind == "reconciliation" {
		return domain.ErrAssetInvalid
	}
	if a.Kind == "property" && s.Amount.MinorUnits < 0 {
		return domain.ErrAssetInvalid
	}
	return nil
}
func (r *AssetRepo) CreateSnapshot(ctx context.Context, s domain.AssetSnapshot, key string) (domain.AssetSnapshot, error) {
	return financialWrite(ctx, r.pool, s.TenantID, "create_snapshot", key, s, func(ctx context.Context) (domain.AssetSnapshot, error) {
		a, err := r.getAccountRow(ctx, s.TenantID, s.AccountID, true)
		if err != nil {
			return s, err
		}
		if err = checkSnapshotAccount(a, s); err != nil {
			return s, err
		}
		var hasOpening bool
		err = r.pool.query(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM asset_snapshots WHERE tenant_id=$1 AND account_id=$2 AND currency_code=$3)`, s.TenantID, s.AccountID, s.Amount.CurrencyCode).Scan(&hasOpening)
		if err != nil {
			return s, err
		}
		if s.Kind == "opening" && hasOpening {
			return s, domain.ErrAssetPrecondition
		}
		if !hasOpening {
			s.Kind = "opening"
		}
		return r.insertSnapshot(ctx, s)
	})
}
func (r *AssetRepo) getSnapshot(ctx context.Context, tenant, id string) (domain.AssetSnapshot, error) {
	return scanSnapshot(r.pool.query(ctx).QueryRow(ctx, `SELECT `+snapshotColumns+` FROM asset_snapshots WHERE tenant_id=$1 AND id=$2`, tenant, id))
}
func (r *AssetRepo) UpdateSnapshot(ctx context.Context, s domain.AssetSnapshot) (domain.AssetSnapshot, error) {
	return financialWrite(ctx, r.pool, s.TenantID, "update_snapshot", "", nil, func(ctx context.Context) (domain.AssetSnapshot, error) {
		old, err := r.getSnapshot(ctx, s.TenantID, s.ID)
		if err != nil {
			return s, err
		}
		a, err := r.getAccountRow(ctx, s.TenantID, old.AccountID, true)
		if err != nil {
			return s, err
		}
		old, err = r.getSnapshot(ctx, s.TenantID, s.ID)
		if err != nil {
			return s, err
		}
		if old.Version != s.Version {
			return s, domain.ErrAssetConflict
		}
		if old.AccountID != s.AccountID || old.Amount.CurrencyCode != s.Amount.CurrencyCode || old.Kind != s.Kind {
			return s, domain.ErrAssetInvalid
		}
		if err = checkSnapshotAccount(a, s); err != nil {
			return s, err
		}
		return scanSnapshot(r.pool.query(ctx).QueryRow(ctx, `UPDATE asset_snapshots SET amount_minor=$3,as_of=$4,note=$5,version=version+1 WHERE tenant_id=$1 AND id=$2 RETURNING `+snapshotColumns, s.TenantID, s.ID, s.Amount.MinorUnits, s.AsOf, s.Note))
	})
}
func (r *AssetRepo) DeleteSnapshot(ctx context.Context, tenant, id string, version int64) error {
	_, err := financialWrite(ctx, r.pool, tenant, "delete_snapshot", "", nil, func(ctx context.Context) (bool, error) {
		s, e := r.getSnapshot(ctx, tenant, id)
		if e != nil {
			return false, e
		}
		a, e := r.getAccountRow(ctx, tenant, s.AccountID, true)
		if e != nil {
			return false, e
		}
		if a.Archived {
			return false, domain.ErrAssetPrecondition
		}
		s, e = r.getSnapshot(ctx, tenant, id)
		if e != nil {
			return false, e
		}
		if s.Version != version {
			return false, domain.ErrAssetConflict
		}
		var orphan bool
		e = r.pool.query(ctx).QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM asset_snapshots WHERE tenant_id=$1 AND account_id=$2 AND currency_code=$3 AND id<>$4)
            AND EXISTS(SELECT 1 FROM asset_movements WHERE tenant_id=$1 AND account_id=$2 AND currency_code=$3)`, tenant, s.AccountID, s.Amount.CurrencyCode, id).Scan(&orphan)
		if e != nil {
			return false, e
		}
		if orphan {
			return false, domain.ErrAssetPrecondition
		}
		_, e = r.pool.query(ctx).Exec(ctx, `DELETE FROM asset_snapshots WHERE tenant_id=$1 AND id=$2`, tenant, id)
		return e == nil, e
	})
	return err
}

const transferColumns = `id,tenant_id,COALESCE(user_id::text,''),COALESCE(from_account_id::text,''),COALESCE(to_account_id::text,''),from_amount_minor,from_currency_code,to_amount_minor,to_currency_code,occurred_at,note,version,created_at`

func scanTransfer(row interface{ Scan(...any) error }) (domain.AssetTransfer, error) {
	var t domain.AssetTransfer
	err := row.Scan(&t.ID, &t.TenantID, &t.UserID, &t.FromAccountID, &t.ToAccountID, &t.FromAmount.MinorUnits, &t.FromAmount.CurrencyCode, &t.ToAmount.MinorUnits, &t.ToAmount.CurrencyCode, &t.OccurredAt, &t.Note, &t.Version, &t.CreatedAt)
	return t, err
}
func (r *AssetRepo) getTransfer(ctx context.Context, tenant, id string, lock bool) (domain.AssetTransfer, error) {
	sql := `SELECT ` + transferColumns + ` FROM asset_transfers WHERE tenant_id=$1 AND id=$2`
	if lock {
		sql += ` FOR UPDATE`
	}
	return scanTransfer(r.pool.query(ctx).QueryRow(ctx, sql, tenant, id))
}
func (r *AssetRepo) validateTransferAccounts(ctx context.Context, t domain.AssetTransfer) error {
	type leg struct{ id, currency string }
	legs := []leg{}
	if t.FromAccountID != "" {
		legs = append(legs, leg{t.FromAccountID, t.FromAmount.CurrencyCode})
	}
	if t.ToAccountID != "" {
		legs = append(legs, leg{t.ToAccountID, t.ToAmount.CurrencyCode})
	}
	sort.Slice(legs, func(i, j int) bool { return legs[i].id < legs[j].id })
	investment := false
	for _, l := range legs {
		kind, err := validateAssetLeg(ctx, r.pool, t.TenantID, l.id, l.currency, false)
		if err != nil {
			return err
		}
		investment = investment || kind == "investment"
	}
	if !investment && (t.FromAmount != t.ToAmount) {
		return domain.ErrAssetInvalid
	}
	return nil
}
func (r *AssetRepo) transferMovements(ctx context.Context, t domain.AssetTransfer) error {
	_, err := r.pool.query(ctx).Exec(ctx, `DELETE FROM asset_movements WHERE tenant_id=$1 AND transfer_id=$2`, t.TenantID, t.ID)
	if err != nil {
		return err
	}
	for _, l := range []struct {
		id    string
		money domain.Money
		sign  int64
		leg   string
	}{{t.FromAccountID, t.FromAmount, -1, "from"}, {t.ToAccountID, t.ToAmount, 1, "to"}} {
		if l.id == "" {
			continue
		}
		_, err = r.pool.query(ctx).Exec(ctx, `INSERT INTO asset_movements(tenant_id,account_id,currency_code,amount_minor,occurred_at,transfer_id,leg) VALUES($1,$2,$3,$4,$5,$6,$7)`, t.TenantID, l.id, l.money.CurrencyCode, l.money.MinorUnits*l.sign, t.OccurredAt, t.ID, l.leg)
		if err != nil {
			return err
		}
	}
	return nil
}
func (r *AssetRepo) CreateTransfer(ctx context.Context, t domain.AssetTransfer) (domain.AssetTransfer, error) {
	return financialWrite(ctx, r.pool, t.TenantID, "create_transfer", t.RequestKey, t, func(ctx context.Context) (domain.AssetTransfer, error) {
		if err := r.validateTransferAccounts(ctx, t); err != nil {
			return t, err
		}
		created, err := scanTransfer(r.pool.query(ctx).QueryRow(ctx, `INSERT INTO asset_transfers(tenant_id,user_id,from_account_id,to_account_id,from_amount_minor,from_currency_code,to_amount_minor,to_currency_code,occurred_at,note)
            VALUES($1,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10) RETURNING `+transferColumns, t.TenantID, t.UserID, t.FromAccountID, t.ToAccountID, t.FromAmount.MinorUnits, t.FromAmount.CurrencyCode, t.ToAmount.MinorUnits, t.ToAmount.CurrencyCode, t.OccurredAt, t.Note))
		if err != nil {
			return t, err
		}
		if err = r.transferMovements(ctx, created); err != nil {
			return t, err
		}
		return created, nil
	})
}
func (r *AssetRepo) UpdateTransfer(ctx context.Context, t domain.AssetTransfer) (domain.AssetTransfer, error) {
	return financialWrite(ctx, r.pool, t.TenantID, "update_transfer", "", nil, func(ctx context.Context) (domain.AssetTransfer, error) {
		old, err := r.getTransfer(ctx, t.TenantID, t.ID, true)
		if err != nil {
			return t, err
		}
		if old.Version != t.Version {
			return t, domain.ErrAssetConflict
		}
		// Serialize all old and new account legs in one stable order.
		ids := map[string]bool{}
		for _, id := range []string{old.FromAccountID, old.ToAccountID, t.FromAccountID, t.ToAccountID} {
			if id != "" {
				ids[id] = true
			}
		}
		sorted := []string{}
		for id := range ids {
			sorted = append(sorted, id)
		}
		sort.Strings(sorted)
		for _, id := range sorted {
			a, e := r.getAccountRow(ctx, t.TenantID, id, true)
			if e != nil {
				return t, e
			}
			if a.Archived {
				return t, domain.ErrAssetPrecondition
			}
		}
		if err = r.validateTransferAccounts(ctx, t); err != nil {
			return t, err
		}
		updated, err := scanTransfer(r.pool.query(ctx).QueryRow(ctx, `UPDATE asset_transfers SET from_account_id=NULLIF($3,'')::uuid,to_account_id=NULLIF($4,'')::uuid,from_amount_minor=$5,from_currency_code=$6,to_amount_minor=$7,to_currency_code=$8,occurred_at=$9,note=$10,version=version+1 WHERE tenant_id=$1 AND id=$2 RETURNING `+transferColumns, t.TenantID, t.ID, t.FromAccountID, t.ToAccountID, t.FromAmount.MinorUnits, t.FromAmount.CurrencyCode, t.ToAmount.MinorUnits, t.ToAmount.CurrencyCode, t.OccurredAt, t.Note))
		if err != nil {
			return t, err
		}
		if err = r.transferMovements(ctx, updated); err != nil {
			return t, err
		}
		return updated, nil
	})
}
func (r *AssetRepo) DeleteTransfer(ctx context.Context, tenant, id string, version int64) error {
	_, err := financialWrite(ctx, r.pool, tenant, "delete_transfer", "", nil, func(ctx context.Context) (bool, error) {
		t, e := r.getTransfer(ctx, tenant, id, true)
		if e != nil {
			return false, e
		}
		if t.Version != version {
			return false, domain.ErrAssetConflict
		}
		ids := []string{t.FromAccountID, t.ToAccountID}
		sort.Strings(ids)
		for _, accountID := range ids {
			if accountID == "" {
				continue
			}
			a, err := r.getAccountRow(ctx, tenant, accountID, true)
			if err != nil {
				return false, err
			}
			if a.Archived {
				return false, domain.ErrAssetPrecondition
			}
		}
		_, e = r.pool.query(ctx).Exec(ctx, `DELETE FROM asset_transfers WHERE tenant_id=$1 AND id=$2`, tenant, id)
		return e == nil, e
	})
	return err
}

const historySQL = `
    SELECT s.id,s.kind,s.amount_minor,s.currency_code,s.as_of,s.note,s.id AS source_id,'' AS counterparty,COALESCE(s.user_id::text,'') AS user_id,s.version
    FROM asset_snapshots s WHERE s.tenant_id=$1 AND s.account_id=$2
    UNION ALL
    SELECT m.id,CASE WHEN m.transaction_id IS NOT NULL THEN tr.type::text WHEN m.exchange_id IS NOT NULL THEN 'exchange' ELSE 'transfer' END,
        m.amount_minor,m.currency_code,m.occurred_at,COALESCE(tr.comment,ex.note,at.note,''),COALESCE(m.transaction_id,m.exchange_id,m.transfer_id),
        COALESCE(other.name,CASE WHEN m.transfer_id IS NOT NULL THEN 'outside' ELSE '' END),
        COALESCE(tr.user_id::text,ex.user_id::text,at.user_id::text,''),COALESCE(at.version,0)
    FROM asset_movements m
    LEFT JOIN transactions tr ON tr.id=m.transaction_id
    LEFT JOIN currency_exchanges ex ON ex.id=m.exchange_id
    LEFT JOIN asset_transfers at ON at.id=m.transfer_id
    LEFT JOIN asset_accounts other ON other.id=CASE WHEN m.leg='from' THEN COALESCE(ex.to_asset_account_id,at.to_account_id) ELSE COALESCE(ex.from_asset_account_id,at.from_account_id) END
    WHERE m.tenant_id=$1 AND m.account_id=$2`

func (r *AssetRepo) ListAccountHistory(ctx context.Context, tenant, id string, page, size int) ([]domain.AssetHistoryItem, int64, error) {
	if _, err := r.getAccountRow(ctx, tenant, id, false); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 25
	}
	var total int64
	if err := r.pool.query(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM (`+historySQL+`) h`, tenant, id).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.query(ctx).Query(ctx, `SELECT * FROM (`+historySQL+`) h ORDER BY as_of DESC,id DESC LIMIT $3 OFFSET $4`, tenant, id, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	out := []domain.AssetHistoryItem{}
	for rows.Next() {
		var h domain.AssetHistoryItem
		if err = rows.Scan(&h.ID, &h.Kind, &h.Amount.MinorUnits, &h.Amount.CurrencyCode, &h.OccurredAt, &h.Note, &h.SourceID, &h.CounterpartyName, &h.UserID, &h.Version); err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		h := &out[i]
		switch h.Kind {
		case "opening", "reconciliation", "valuation":
			s, e := r.getSnapshot(ctx, tenant, h.SourceID)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return nil, 0, e
			}
			if e == nil {
				h.Snapshot = &s
				var amount string
				e = r.pool.query(ctx).QueryRow(ctx, `WITH previous AS (
                    SELECT amount_minor,as_of FROM asset_snapshots
                    WHERE tenant_id=$1 AND account_id=$2 AND currency_code=$3 AND as_of<$4
                    ORDER BY as_of DESC LIMIT 1
                ) SELECT (p.amount_minor::numeric + COALESCE((SELECT SUM(amount_minor::numeric)
                    FROM asset_movements WHERE tenant_id=$1 AND account_id=$2 AND currency_code=$3
                    AND occurred_at>p.as_of AND occurred_at<=$4),0))::text FROM previous p`,
					tenant, s.AccountID, s.Amount.CurrencyCode, s.AsOf).Scan(&amount)
				if e != nil && !errors.Is(e, pgx.ErrNoRows) {
					return nil, 0, e
				}
				if e == nil {
					n, err := parseMinorInteger(amount)
					if err != nil {
						return nil, 0, err
					}
					h.CalculatedAmount = &domain.Money{CurrencyCode: s.Amount.CurrencyCode, MinorUnits: n}
				}

			}
		case "transfer":
			t, e := r.getTransfer(ctx, tenant, h.SourceID, false)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return nil, 0, e
			}
			if e == nil {
				h.Transfer = &t
			}
		}
	}
	return out, total, nil
}
