package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
)

type dbQuery interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type financialTxKey struct{}

func requestInContext(ctx context.Context) ctxutil.FinancialRequest {
	return ctxutil.FinancialRequestFromContext(ctx)
}
func (p *Pool) query(ctx context.Context) dbQuery {
	if tx, ok := ctx.Value(financialTxKey{}).(pgx.Tx); ok {
		return tx
	}
	return p.DB
}
func activeTenant(ctx context.Context) string { id, _ := ctxutil.TenantIDFromContext(ctx); return id }
func lockClause(lock bool) string {
	if lock {
		return " FOR UPDATE"
	}
	return ""
}
func lockAssetAccounts(ctx context.Context, pool *Pool, tenant string, ids ...string) error {
	unique := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			unique[id] = true
		}
	}
	sorted := []string{}
	for id := range unique {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	repo := NewAssetRepo(pool)
	for _, id := range sorted {
		a, err := repo.getAccountRow(ctx, tenant, id, true)
		if err != nil {
			return err
		}
		if a.Archived {
			return domain.ErrAssetPrecondition
		}
	}
	return nil
}
func parseMinorInteger(value string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: balance exceeds supported range", domain.ErrAssetInvalid)
	}
	return n, nil
}

// Every source and all its ledger legs share a transaction, including idempotency results.
func financialWrite[T any](ctx context.Context, pool *Pool, tenant, operation, key string, payload any, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	if len(key) > 128 {
		return zero, domain.ErrAssetInvalid
	}
	if _, ok := ctx.Value(financialTxKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	tx, err := pool.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ctx = context.WithValue(ctx, financialTxKey{}, tx)
	var hash string
	if key != "" {
		encoded, e := json.Marshal(payload)
		if e != nil {
			return zero, e
		}
		sum := sha256.Sum256(encoded)
		hash = hex.EncodeToString(sum[:])
		// Serialize identical keys before checking the stored result.
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, tenant+":"+operation+":"+key); err != nil {
			return zero, err
		}
		var existingHash string
		var result []byte
		err = tx.QueryRow(ctx, `SELECT payload_hash, result FROM financial_write_requests WHERE tenant_id=$1 AND operation=$2 AND request_key=$3`, tenant, operation, key).Scan(&existingHash, &result)
		if err == nil {
			if existingHash != hash {
				return zero, domain.ErrIdempotencyConflict
			}
			if err = json.Unmarshal(result, &zero); err != nil {
				return zero, err
			}
			if err = tx.Commit(ctx); err != nil {
				return zero, err
			}
			return zero, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return zero, err
		}
	}
	result, err := fn(ctx)
	if err != nil {
		return zero, err
	}
	if key != "" {
		encoded, e := json.Marshal(result)
		if e != nil {
			return zero, e
		}
		if _, err = tx.Exec(ctx, `INSERT INTO financial_write_requests(tenant_id,operation,request_key,payload_hash,result) VALUES($1,$2,$3,$4,$5)`, tenant, operation, key, hash, encoded); err != nil {
			return zero, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}

// Account locks coordinate movements, archive and reconciliations. Call in sorted ID order.
func validateAssetLeg(ctx context.Context, pool *Pool, tenant, id, currency string, monetary bool) (string, error) {
	var kind, fixed string
	var archived bool
	err := pool.query(ctx).QueryRow(ctx, `SELECT kind, COALESCE(fixed_currency_code,''), archived FROM asset_accounts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&kind, &fixed, &archived)
	if err != nil {
		return "", err
	}
	if archived {
		return "", domain.ErrAssetPrecondition
	}
	if kind == "property" {
		return "", fmt.Errorf("%w: property uses manual valuations only", domain.ErrAssetInvalid)
	}
	if monetary && kind == "investment" {
		return "", fmt.Errorf("%w: investment accounts use transfers and whole-account valuations", domain.ErrAssetInvalid)
	}
	if fixed != "" && fixed != currency {
		return "", fmt.Errorf("%w: currency does not match account", domain.ErrAssetInvalid)
	}
	var initialized bool
	err = pool.query(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM asset_snapshots WHERE tenant_id=$1 AND account_id=$2 AND currency_code=$3)`, tenant, id, currency).Scan(&initialized)
	if err != nil {
		return "", err
	}
	if !initialized {
		return "", domain.ErrAssetPrecondition
	}
	return kind, nil
}
