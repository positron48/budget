package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"reflect"
	"time"
)

func sameLedgerMetadata(a, b d.InvestmentLedger) bool {
	a.StartedAt = a.StartedAt.UTC()
	b.StartedAt = b.StartedAt.UTC()
	a.CreatedAt = a.CreatedAt.UTC()
	b.CreatedAt = b.CreatedAt.UTC()
	a.Version = 0
	b.Version = 0
	return reflect.DeepEqual(a, b)
}
func recordLedgerRevision(ctx context.Context, tx pgx.Tx, l d.InvestmentLedger, u, action, reason string, before *d.InvestmentLedger) error {
	// SQL-shaped JSON also matches migration baselines and portable exports.
	var b any
	if before != nil {
		b = ledgerRevisionJSON(*before)
	}
	_, e := tx.Exec(ctx, `INSERT INTO investment_ledger_revisions(tenant_id,ledger_id,version,action,actor_id,reason,before_data,after_data) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, l.TenantID, l.ID, l.MetadataVersion, action, u, reason, b, ledgerRevisionJSON(l))
	return e
}
func ledgerRevisionJSON(l d.InvestmentLedger) map[string]any {
	return map[string]any{"id": l.ID, "tenant_id": l.TenantID, "name": l.Name, "started_at": l.StartedAt, "version": l.Version, "created_at": l.CreatedAt, "broker": l.Broker, "account_type": l.AccountType, "comment": l.Comment, "default_currency": l.DefaultCurrency, "archived": l.Archived, "metadata_version": l.MetadataVersion}
}
func parseLedgerRevision(raw []byte) (*d.InvestmentLedger, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var v struct {
		ID              string    `json:"id"`
		Tenant          string    `json:"tenant_id"`
		Name            string    `json:"name"`
		Started         time.Time `json:"started_at"`
		Version         int64     `json:"version"`
		Created         time.Time `json:"created_at"`
		Broker          string    `json:"broker"`
		AccountType     string    `json:"account_type"`
		Comment         string    `json:"comment"`
		DefaultCurrency string    `json:"default_currency"`
		Archived        bool      `json:"archived"`
		MetadataVersion int64     `json:"metadata_version"`
	}
	if e := json.Unmarshal(raw, &v); e != nil {
		return nil, e
	}
	return &d.InvestmentLedger{ID: v.ID, TenantID: v.Tenant, Name: v.Name, StartedAt: v.Started, Version: v.Version, CreatedAt: v.Created, Broker: v.Broker, AccountType: v.AccountType, Comment: v.Comment, DefaultCurrency: v.DefaultCurrency, Archived: v.Archived, MetadataVersion: v.MetadataVersion}, nil
}
func (r *InvestmentRepo) ListLedgersWithArchive(ctx context.Context, t string, limit, offset int, include bool) ([]d.InvestmentLedger, error) {
	rows, e := r.pool.DB.Query(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND ($4 OR NOT archived) ORDER BY created_at,id LIMIT $2 OFFSET $3`, t, limit, offset, include)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []d.InvestmentLedger{}
	for rows.Next() {
		l, e := scanLedger(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func (r *InvestmentRepo) UpdateLedger(ctx context.Context, t, u string, m inv.LedgerUpdate) (d.InvestmentLedger, error) {
	var out d.InvestmentLedger
	m, e := inv.NormalizeLedgerUpdate(m)
	if e != nil {
		return out, e
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = lockInvestment(ctx, tx, t); e != nil {
		return out, e
	}
	var role string
	if e = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, t, u).Scan(&role); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return out, d.ErrInvestmentForbidden
		}
		return out, e
	}
	if role != "owner" && role != "admin" {
		return out, d.ErrInvestmentForbidden
	}
	input := m
	input.RequestKey = ""
	hash := investmentHash(input)
	retry, e := investmentRetry(ctx, tx, t, "investment.ledger.update", m.RequestKey, hash)
	if e != nil {
		return out, e
	}
	if retry != "" {
		return scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, t, retry))
	}
	out, e = scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, m.ID))
	if e != nil {
		return out, e
	}
	if out.MetadataVersion != m.ExpectedMetadataVersion {
		return out, d.ErrInvestmentConflict
	}
	before := out
	switch m.Action {
	case "edit":
		out.Name = m.Name
		out.Broker = m.Broker
		out.AccountType = m.AccountType
		out.Comment = m.Comment
		out.DefaultCurrency = m.DefaultCurrency
	case "archive":
		if out.Archived {
			return out, d.ErrInvestmentConflict
		}
		if out.Version != m.ExpectedLedgerVersion {
			return out, d.ErrInvestmentConflict
		}
		ops, e := loadInvestmentOperations(ctx, tx, t, out.ID)
		if e != nil {
			return out, e
		}
		at := time.Now().UTC()
		result, e := replayInvestmentLedgerTx(ctx, tx, t, out, ops, at)
		if e != nil {
			return out, e
		}
		if e = inv.CheckLedgerClosure(result.Snapshot, ops, at); e != nil {
			return out, e
		}
		out.Archived = true
	case "restore":
		if !out.Archived {
			return out, d.ErrInvestmentConflict
		}
		out.Archived = false
	}
	out.MetadataVersion++
	out, e = scanLedger(tx.QueryRow(ctx, `UPDATE investment_ledgers SET name=$3,broker=$4,account_type=$5,comment=$6,default_currency=$7,archived=$8,metadata_version=$9 WHERE tenant_id=$1 AND id=$2 RETURNING `+ledgerColumns, t, out.ID, out.Name, out.Broker, out.AccountType, out.Comment, out.DefaultCurrency, out.Archived, out.MetadataVersion))
	if e != nil {
		return out, e
	}
	if e = recordLedgerRevision(ctx, tx, out, u, m.Action, m.Reason, &before); e != nil {
		return out, e
	}
	if e = saveInvestmentRequest(ctx, tx, t, "investment.ledger.update", m.RequestKey, hash, out.ID); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (r *InvestmentRepo) LedgerHistory(ctx context.Context, t, u string, q inv.LedgerHistoryQuery) (inv.LedgerHistory, error) {
	var out inv.LedgerHistory
	q, e := inv.NormalizeLedgerHistory(q)
	if e != nil {
		return out, e
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = checkJournalReader(ctx, tx, t, u); e != nil {
		return out, e
	}
	out.Ledger, e = scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, t, q.ID))
	if e != nil {
		return out, e
	}
	if q.ExpectedMetadataVersion > 0 && out.Ledger.MetadataVersion != q.ExpectedMetadataVersion {
		return out, d.ErrInvestmentConflict
	}
	if q.AfterVersion >= out.Ledger.MetadataVersion {
		return out, d.ErrInvestmentInvalid
	}
	rows, e := tx.Query(ctx, `SELECT version,action,actor_id,reason,recorded_at,before_data,after_data FROM investment_ledger_revisions WHERE tenant_id=$1 AND ledger_id=$2 AND version>$3 ORDER BY version LIMIT $4`, t, q.ID, q.AfterVersion, q.Limit+1)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	out.Revisions = []inv.LedgerRevision{}
	for rows.Next() {
		var v inv.LedgerRevision
		var b, a []byte
		if e = rows.Scan(&v.Version, &v.Action, &v.ActorID, &v.Reason, &v.RecordedAt, &b, &a); e != nil {
			return out, e
		}
		v.Before, e = parseLedgerRevision(b)
		if e != nil {
			return out, e
		}
		v.After, e = parseLedgerRevision(a)
		if e != nil {
			return out, e
		}
		if v.After == nil || v.After.ID != q.ID || v.After.TenantID != t || v.After.MetadataVersion != v.Version || v.Version != q.AfterVersion+int64(len(out.Revisions))+1 {
			return out, d.ErrInvestmentPrecondition
		}
		if v.Version == 1 {
			if (v.Action != "create" && v.Action != "baseline") || v.Before != nil || v.After.Archived {
				return out, d.ErrInvestmentPrecondition
			}
		} else {
			if v.Before == nil || v.Before.ID != q.ID || v.Before.TenantID != t || v.Before.MetadataVersion != v.Version-1 {
				return out, d.ErrInvestmentPrecondition
			}
			switch v.Action {
			case "edit":
				if v.Before.Archived != v.After.Archived {
					return out, d.ErrInvestmentPrecondition
				}
			case "archive":
				if v.Before.Archived || !v.After.Archived {
					return out, d.ErrInvestmentPrecondition
				}
			case "restore":
				if !v.Before.Archived || v.After.Archived {
					return out, d.ErrInvestmentPrecondition
				}
			default:
				return out, d.ErrInvestmentPrecondition
			}
			if len(out.Revisions) > 0 && !sameLedgerMetadata(*v.Before, *out.Revisions[len(out.Revisions)-1].After) {
				return out, d.ErrInvestmentPrecondition
			}
		}
		out.Revisions = append(out.Revisions, v)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	rows.Close()
	if len(out.Revisions) > q.Limit {
		out.Revisions = out.Revisions[:q.Limit]
		out.NextAfterVersion = out.Revisions[len(out.Revisions)-1].Version
	}
	if len(out.Revisions) == 0 || out.NextAfterVersion == 0 && out.Revisions[len(out.Revisions)-1].Version != out.Ledger.MetadataVersion {
		return out, d.ErrInvestmentPrecondition
	}
	if q.AfterVersion > 0 {
		var raw []byte
		if e = tx.QueryRow(ctx, `SELECT after_data FROM investment_ledger_revisions WHERE tenant_id=$1 AND ledger_id=$2 AND version=$3`, t, q.ID, q.AfterVersion).Scan(&raw); e != nil {
			return out, e
		}
		previous, e := parseLedgerRevision(raw)
		if e != nil || previous == nil || out.Revisions[0].Before == nil || !sameLedgerMetadata(*previous, *out.Revisions[0].Before) {
			return out, d.ErrInvestmentPrecondition
		}
	}
	if out.NextAfterVersion == 0 && !sameLedgerMetadata(*out.Revisions[len(out.Revisions)-1].After, out.Ledger) {
		return out, d.ErrInvestmentPrecondition
	}
	return out, tx.Commit(ctx)
}
