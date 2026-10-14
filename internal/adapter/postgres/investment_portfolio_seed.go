package postgres

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
)

// SeedPortfolioCandidateScope is for a newly created, isolated candidate DB.
// A DB containing any tenant/user refuses before changing anything. Account
// identity and metadata history survive; financial journal counters restart.
// Synthetic disabled identities are never copied to the production database.
func (r *InvestmentRepo) SeedPortfolioCandidateScope(ctx context.Context, actor string, before archive.Bundle) error {
	sealed, e := archive.Seal(before.Data)
	if e != nil || before.Format != archive.Format || sealed.SHA256 != before.SHA256 || !slices.Contains(before.Data.Actors, actor) {
		return d.ErrInvestmentPrecondition
	}
	empty := func(tx pgx.Tx) (bool, error) {
		var occupied bool
		e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenants) OR EXISTS(SELECT 1 FROM users)`).Scan(&occupied)
		return !occupied, e
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	available, e := empty(tx)
	if e != nil {
		return e
	}
	if !available {
		return d.ErrInvestmentPrecondition
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE tenants,users IN EXCLUSIVE MODE`); e != nil {
		return e
	}
	available, e = empty(tx)
	if e != nil {
		return e
	}
	if !available {
		return d.ErrInvestmentPrecondition
	}
	if _, e = tx.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Isolated portfolio candidate','RUB')`, before.Data.TenantID); e != nil {
		return e
	}
	for _, id := range before.Data.Actors {
		if _, e = tx.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1::uuid,'candidate-'||($1::uuid)::text||'@example.invalid','disabled')`, id); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, before.Data.TenantID, actor); e != nil {
		return e
	}
	// Preserve schema/account metadata and immutable broker source observations,
	// not credentials, old journals or claims against their operation IDs.
	data := before.Data
	data.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	data.Tables = append([]archive.Table(nil), before.Data.Tables...)
	count := 0
	for i, table := range data.Tables {
		data.Tables[i].Rows = nil
		if table.Name == "investment_ledger_revisions" || table.Name == "investment_broker_snapshots" || table.Name == "investment_broker_observations" || table.Name == "investment_broker_snapshot_rows" {
			data.Tables[i].Rows = append([]json.RawMessage(nil), table.Rows...)
		}
		if table.Name != "investment_ledgers" {
			continue
		}
		for _, raw := range table.Rows {
			var head map[string]json.RawMessage
			if json.Unmarshal(raw, &head) != nil || string(head["portfolio_tracking"]) != "true" {
				return d.ErrInvestmentPrecondition
			}
			head["version"] = json.RawMessage("1")
			head["next_sequence"] = json.RawMessage("1")
			encoded, e := json.Marshal(head)
			if e != nil {
				return e
			}
			data.Tables[i].Rows = append(data.Tables[i].Rows, encoded)
			count++
		}
	}
	if count == 0 {
		return d.ErrInvestmentPrecondition
	}
	archive.Canonicalize(&data)
	candidate, e := archive.Seal(data)
	if e != nil {
		return e
	}
	if _, e = archive.VerifyJournal(candidate.Data); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	// Restoration has its own atomic transaction. A failed isolated build is
	// retained for diagnosis and never selected as a production replacement.
	_, e = r.RestoreInvestmentArchive(ctx, actor, candidate, candidate.SHA256)
	return e
}
