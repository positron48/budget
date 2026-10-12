package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"math/big"
	"slices"
)

const cashTransferHistorySQL = `SELECT kind,id,data FROM (
 SELECT 'snapshot' AS kind,id::text,to_jsonb(s)::text AS data FROM asset_snapshots s WHERE tenant_id=$1 AND account_id=$2
 UNION ALL
 SELECT 'movement' AS kind,id::text,to_jsonb(m)::text AS data FROM asset_movements m WHERE tenant_id=$1 AND account_id=$2
 ) facts ORDER BY kind,id LIMIT 100001`

func cashTransferHistoryHash(ctx context.Context, tx pgx.Tx, t, id string) (string, error) {
	rows, e := tx.Query(ctx, cashTransferHistorySQL, t, id)
	if e != nil {
		return "", e
	}
	defer rows.Close()
	h := sha256.New()
	count := 0
	for rows.Next() {
		count++
		if count > 100000 {
			return "", d.ErrInvestmentLimit
		}
		var kind, id, raw string
		if e = rows.Scan(&kind, &id, &raw); e != nil {
			return "", e
		}
		h.Write([]byte(kind + ":" + id + ":" + raw + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), rows.Err()
}
func (r *InvestmentRepo) PreviewCashTransfer(ctx context.Context, t, u string, m inv.CashTransferMutation) (inv.CashTransferPreview, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return inv.CashTransferPreview{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = checkJournalReader(ctx, tx, t, u); e != nil {
		return inv.CashTransferPreview{}, e
	}
	prepared, plan, e := r.prepareCashTransferTx(ctx, tx, t, u, m, false)
	if e != nil {
		return inv.CashTransferPreview{}, e
	}
	out, e := r.cashTransferPreviewTx(ctx, tx, t, u, m, prepared, plan)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (r *InvestmentRepo) cashTransferPreviewTx(ctx context.Context, tx pgx.Tx, t, u string, m inv.CashTransferMutation, p cashTransferPlan, plan linkedPreviewPlan) (inv.CashTransferPreview, error) {
	out := inv.CashTransferPreview{Transfer: p.value, Accounts: []inv.AssetTransferComparison{}}
	m.PreviewHash = ""
	history, e := cashTransferHistoryHash(ctx, tx, t, p.ordinary.ID)
	if e != nil {
		return out, e
	}
	command := struct {
		Mutation inv.CashTransferMutation
		From, To d.AssetAccount
		History  string
	}{m, p.from, p.to, history}
	out.Preview, e = r.linkedFinancialPreview(ctx, tx, t, u, command, plan)
	if e != nil || !out.Preview.Valid {
		return out, e
	}
	ctx = context.WithValue(ctx, financialTxKey{}, tx)
	assets := NewAssetRepo(r.pool)
	currencies := []string{p.bankMoney.CurrencyCode}
	oldMoney := p.old.Transfer.FromAmount
	if !p.inbound {
		oldMoney = p.old.Transfer.ToAmount
	}
	if p.old.Transfer.ID != "" && !slices.Contains(currencies, oldMoney.CurrencyCode) {
		currencies = append(currencies, oldMoney.CurrencyCode)
	}
	slices.Sort(currencies)
	seen := map[string]bool{}
	for _, point := range out.Preview.Comparisons {
		key := point.Stage + point.At.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		balances, e := assets.balances(ctx, t, p.ordinary.ID, point.At)
		if e != nil {
			return out, e
		}
		for _, c := range currencies {
			row := inv.AssetTransferComparison{Stage: point.Stage, At: point.At, AccountID: p.ordinary.ID, Currency: c}
			for _, b := range balances[p.ordinary.ID] {
				if b.Amount.CurrencyCode != c {
					continue
				}
				row.BeforeKnown = true
				row.AfterKnown = true
				row.BeforeMinor = b.Amount.MinorUnits
				amount := big.NewInt(b.Amount.MinorUnits)
				sign := int64(1)
				if p.inbound {
					sign = -1
				}
				if p.old.Transfer.ID != "" && oldMoney.CurrencyCode == c && p.old.Transfer.OccurredAt.After(b.ConfirmedAt) && !p.old.Transfer.OccurredAt.After(point.At) {
					amount.Sub(amount, new(big.Int).Mul(big.NewInt(sign), big.NewInt(oldMoney.MinorUnits)))
				}
				if p.value.Status == "posted" && p.bankMoney.CurrencyCode == c && p.value.Transfer.OccurredAt.After(b.ConfirmedAt) && !p.value.Transfer.OccurredAt.After(point.At) {
					amount.Add(amount, new(big.Int).Mul(big.NewInt(sign), big.NewInt(p.bankMoney.MinorUnits)))
				}
				if !amount.IsInt64() {
					return out, d.ErrInvestmentInvalid
				}
				row.AfterMinor = amount.Int64()
			}
			out.Accounts = append(out.Accounts, row)
		}
	}
	return out, nil
}
