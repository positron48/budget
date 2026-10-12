package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

const assetInvestmentFXPolicy = "assets_cbr"

func readAssetInvestmentFX(ctx context.Context, tx pgx.Tx, pool *Pool, snapshots []d.InvestmentSnapshot, target string, at time.Time) ([]calc.FXFixing, error) {
	codes := map[string]bool{}
	for _, s := range snapshots {
		for _, c := range s.Cash {
			codes[c.Currency] = true
		}
		for _, p := range s.Positions {
			codes[p.Currency] = true
		}
	}
	list := []string{}
	for c := range codes {
		if c != target {
			list = append(list, c)
		}
	}
	sort.Strings(list)
	ctx = context.WithValue(ctx, financialTxKey{}, tx)
	rates, e := NewFxRepo(pool).GetAssetRates(ctx, list, target, at)
	if e != nil {
		return nil, e
	}
	loc, e := time.LoadLocation("Europe/Moscow")
	if e != nil {
		return nil, e
	}
	out := []calc.FXFixing{}
	for _, r := range rates {
		// AssetService's convention is the effective Moscow calendar date, with
		// nearest prior CBR and same-date manual priority, not market-history FX.
		date, e := time.ParseInLocation(time.DateOnly, r.AsOf.Format(time.DateOnly), loc)
		if e != nil {
			return nil, e
		}
		out = append(out, calc.FXFixing{From: r.From, To: r.To, Rate: r.RateDecimal, Policy: assetInvestmentFXPolicy, ID: r.Provider + ":" + r.AsOf.Format(time.DateOnly) + ":" + r.From + "/" + r.To + ":" + r.RateDecimal, Revision: 1, At: date, AvailableAt: date, ValidUntil: at.Add(time.Microsecond)})
	}
	return out, nil
}

func investmentMinor(value *big.Rat) (int64, error) {
	if value == nil {
		return 0, d.ErrInvestmentPrecondition
	}
	r := new(big.Rat).Mul(value, big.NewRat(100, 1))
	n := new(big.Int).Abs(r.Num())
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, r.Denom(), rem)
	if rem.Lsh(rem, 1).Cmp(r.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if r.Sign() < 0 {
		q.Neg(q)
	}
	if !q.IsInt64() {
		return 0, d.ErrAssetInvalid
	}
	return q.Int64(), nil
}

func withAssetRead[T any](ctx context.Context, pool *Pool, fn func(context.Context) (T, error)) (T, error) {
	if _, ok := ctx.Value(financialTxKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	var zero T
	tx, e := pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return zero, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, e := fn(context.WithValue(ctx, financialTxKey{}, tx))
	if e != nil {
		return result, e
	}
	return result, tx.Commit(ctx)
}

func (r *AssetRepo) resolveInvestmentAccount(ctx context.Context, a d.AssetAccount, at time.Time) (d.AssetAccount, error) {
	if a.Kind != "investment" {
		return a, nil
	}
	a.ValuationStatus = "manual"
	if a.InvestmentMode != "ledger" || at.Before(a.LedgerValuationFrom) {
		return a, nil
	}
	tx, ok := ctx.Value(financialTxKey{}).(pgx.Tx)
	if !ok {
		return a, d.ErrInvestmentPrecondition
	}
	user, ok := ctxutil.UserIDFromContext(ctx)
	if !ok {
		return a, d.ErrInvestmentForbidden
	}
	at = at.UTC().Truncate(time.Microsecond)
	result, e := readInvestmentValuationPolicy(ctx, tx, nav.Request{TenantID: a.TenantID, UserID: user, LedgerIDs: []string{a.InvestmentLedgerID}, At: at, Currency: a.FixedCurrencyCode, FXPolicyKey: assetInvestmentFXPolicy, ConventionKey: a.InvestmentConventionKey}, nil, r.pool)
	if e != nil {
		return a, e
	}
	minor, e := investmentMinor(result.KnownValue)
	if e != nil {
		return a, e
	}
	a.Balances = []d.AssetBalance{{Amount: d.Money{CurrencyCode: a.FixedCurrencyCode, MinorUnits: minor}}}
	a.ValuationAsOf = at
	a.ValuationStatus = "complete"
	a.ValuationIncomplete = !result.Complete
	if !result.Complete {
		a.ValuationStatus = "incomplete"
	}
	for _, issue := range result.Issues {
		a.ValuationIssues = append(a.ValuationIssues, issue.Code)
	}
	sort.Strings(a.ValuationIssues)
	return a, nil
}

func previewAssetActivationTx(ctx context.Context, tx pgx.Tx, pool *Pool, t, u string, m d.InvestmentAssetActivation) (d.InvestmentAssetActivationPreview, error) {
	out := d.InvestmentAssetActivationPreview{ValuationFrom: m.ValuationFrom}
	var role string
	if e := tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, t, u).Scan(&role); e != nil {
		if e == pgx.ErrNoRows {
			return out, d.ErrInvestmentForbidden
		}
		return out, e
	}
	if role != "owner" && role != "admin" {
		return out, d.ErrInvestmentForbidden
	}
	ctx = ctxutil.WithUserID(context.WithValue(ctx, financialTxKey{}, tx), u)
	account, e := NewAssetRepo(pool).GetAccount(ctx, t, m.AccountID, m.ValuationFrom)
	if e != nil {
		return out, investmentError(e)
	}
	if account.Kind != "investment" || account.Archived || account.InvestmentMode != "manual" {
		return out, d.ErrInvestmentPrecondition
	}
	ledger, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, t, m.LedgerID))
	if e != nil {
		return out, investmentError(e)
	}
	if ledger.StartedAt.After(m.ValuationFrom) {
		return out, d.ErrInvestmentPrecondition
	}
	var used bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM asset_accounts WHERE tenant_id=$1 AND investment_ledger_id=$2)`, t, ledger.ID).Scan(&used); e != nil {
		return out, e
	}
	if used {
		return out, d.ErrInvestmentPrecondition
	}
	if len(account.Balances) != 1 || account.Balances[0].Amount.CurrencyCode != account.FixedCurrencyCode {
		return out, d.ErrInvestmentPrecondition
	}
	out.Account = account
	out.Ledger = ledger
	out.ManualValue = account.Balances[0].Amount
	// Every existing manual fact participates in the preview fence. Movements
	// at/after cutover need reconciliation, not silently ignoring one side.
	rows, e := tx.Query(ctx, `SELECT data,at,is_movement FROM (
 SELECT to_jsonb(s) data,as_of at,false is_movement FROM asset_snapshots s WHERE tenant_id=$1 AND account_id=$2
 UNION ALL SELECT to_jsonb(m),occurred_at,true FROM asset_movements m WHERE tenant_id=$1 AND account_id=$2
 ) f ORDER BY at,data::text LIMIT 10001`, t, account.ID)
	if e != nil {
		return out, e
	}
	inventory := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		var at time.Time
		var movement bool
		if e = rows.Scan(&raw, &at, &movement); e != nil {
			rows.Close()
			return out, e
		}
		inventory = append(inventory, json.RawMessage(raw))
		if movement && !at.Before(m.ValuationFrom) || !movement && at.After(m.ValuationFrom) {
			out.Issues = append(out.Issues, "manual_facts_after_cutover")
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(inventory) > 10000 {
		return out, d.ErrInvestmentLimit
	}
	out.InventoryCount = int64(len(inventory))
	result, e := readInvestmentValuationPolicy(ctx, tx, nav.Request{TenantID: t, UserID: u, LedgerIDs: []string{ledger.ID}, At: m.ValuationFrom, Currency: account.FixedCurrencyCode, FXPolicyKey: assetInvestmentFXPolicy, ConventionKey: m.ConventionKey}, nil, pool)
	if e != nil {
		return out, e
	}
	minor, e := investmentMinor(result.KnownValue)
	if e != nil {
		return out, e
	}
	out.LedgerKnownValue = d.Money{CurrencyCode: account.FixedCurrencyCode, MinorUnits: minor}
	out.Complete = result.Complete && len(out.Issues) == 0
	out.Matches = out.Complete && out.ManualValue == out.LedgerKnownValue
	for _, issue := range result.Issues {
		out.Issues = append(out.Issues, issue.Code)
	}
	sort.Strings(out.Issues)
	// Price/FX evidence, generation, native revisions and manual facts all fence
	// activation, including fixes that leave the rounded comparison unchanged.
	payload := struct {
		Preview    d.InvestmentAssetActivationPreview
		Inventory  []json.RawMessage
		Result     nav.Result
		Convention string
	}{out, inventory, result, m.ConventionKey}
	raw, e := json.Marshal(payload)
	if e != nil {
		return out, e
	}
	sum := sha256.Sum256(raw)
	out.PreviewHash = hex.EncodeToString(sum[:])
	out.EvidenceJSON = raw

	return out, nil
}

func (r *InvestmentRepo) PreviewAssetActivation(ctx context.Context, t, u string, m d.InvestmentAssetActivation) (d.InvestmentAssetActivationPreview, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return d.InvestmentAssetActivationPreview{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, e := previewAssetActivationTx(ctx, tx, r.pool, t, u, m)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (r *InvestmentRepo) ActivateAsset(ctx context.Context, t, u string, m d.InvestmentAssetActivation) (d.AssetAccount, error) {
	var empty d.AssetAccount
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = lockInvestment(ctx, tx, t); e != nil {
		return empty, e
	}
	if e = groupAccess(ctx, tx, t, u, true); e != nil {
		return empty, e
	}
	// Financial facts are serialized by the tenant lock. Hold the publication
	// generation while comparing so provider corrections cannot commit halfway
	// through the read-committed transaction. Request retries see prior commits.
	var generation int64
	if e = tx.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1 FOR UPDATE`, t).Scan(&generation); e != nil && e != pgx.ErrNoRows {
		return empty, e
	}
	if _, e = scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM asset_accounts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, m.AccountID)); e != nil {
		return empty, e
	}
	if _, e = scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, m.LedgerID)); e != nil {
		return empty, e
	}
	hash := investmentHash(m)
	retry, e := investmentRetry(ctx, tx, t, "asset.investment.activate", m.RequestKey, hash)
	if e != nil {
		return empty, e
	}
	ctx = ctxutil.WithUserID(context.WithValue(ctx, financialTxKey{}, tx), u)
	if retry != "" {
		return NewAssetRepo(r.pool).GetAccount(ctx, t, retry, time.Now())
	}
	out, e := previewAssetActivationTx(ctx, tx, r.pool, t, u, m)
	if e != nil {
		return empty, e
	}
	if out.PreviewHash != m.PreviewHash || out.Account.Version != m.ExpectedAccountVersion || out.Ledger.Version != m.ExpectedLedgerVersion {
		return empty, d.ErrInvestmentConflict
	}
	if !out.Complete || !out.Matches {
		return empty, d.ErrInvestmentPrecondition
	}
	_, e = tx.Exec(ctx, `UPDATE asset_accounts SET investment_mode='ledger',investment_ledger_id=$3,ledger_started_at=$4,ledger_valuation_from=$5,investment_convention_key=$6,version=version+1 WHERE tenant_id=$1 AND id=$2`, t, m.AccountID, m.LedgerID, out.Ledger.StartedAt, m.ValuationFrom, m.ConventionKey)
	if e != nil {
		return empty, e
	}
	raw := out.EvidenceJSON
	_, e = tx.Exec(ctx, `INSERT INTO investment_asset_activations(tenant_id,account_id,ledger_id,actor_id,valuation_from,preview_hash,evidence,comparison) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, t, m.AccountID, m.LedgerID, u, m.ValuationFrom, m.PreviewHash, m.Evidence, raw)
	if e != nil {
		return empty, e
	}
	if e = saveInvestmentRequest(ctx, tx, t, "asset.investment.activate", m.RequestKey, hash, m.AccountID); e != nil {
		return empty, e
	}
	result, e := NewAssetRepo(r.pool).GetAccount(ctx, t, m.AccountID, time.Now())
	if e != nil {
		return empty, e
	}
	return result, tx.Commit(ctx)
}

// Keep fixed-currency NAV and overview conversion in the same MVCC snapshot,
// including a concurrent CBR/manual correction between those two phases.
func (r *AssetRepo) AssetReadSnapshot(ctx context.Context, fn func(context.Context) (d.AssetOverview, error)) (d.AssetOverview, error) {
	return withAssetRead(ctx, r.pool, fn)
}
