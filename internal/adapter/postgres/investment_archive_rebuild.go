package postgres

import (
	"context"
	"encoding/json"
	"math"
	"strconv"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

// The caller holds SHARE ROW EXCLUSIVE on the only archived serial table.
// ALTER SEQUENCE RESTART is transactional; setval would escape a dry-run rollback.
func restartArchiveSequences(ctx context.Context, tx pgx.Tx) error {
	var schema, name string
	err := tx.QueryRow(ctx, `SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid=pg_get_serial_sequence('public.investment_import_mapping_revisions','id')::regclass`).Scan(&schema, &name)
	if err != nil {
		return err
	}
	sequence := pgx.Identifier{schema, name}.Sanitize()
	var last, maximum int64
	var called bool
	if err = tx.QueryRow(ctx, `SELECT last_value,is_called FROM `+sequence).Scan(&last, &called); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM investment_import_mapping_revisions`).Scan(&maximum); err != nil {
		return err
	}
	if maximum == math.MaxInt64 || called && last == math.MaxInt64 {
		return d.ErrInvestmentLimit
	}
	next := last
	if called {
		next++
	}
	if maximum >= next {
		next = maximum + 1
	}
	_, err = tx.Exec(ctx, `ALTER SEQUENCE `+sequence+` RESTART WITH `+strconv.FormatInt(next, 10))
	return err
}

func restoreArchiveProjections(ctx context.Context, tx pgx.Tx, tenant string) error {
	// Current heads and fresh revisions regenerate derived journal work.
	if _, err := tx.Exec(ctx, `INSERT INTO investment_recalculation_jobs(tenant_id,ledger_id,journal_revision,dirty_from) SELECT tenant_id,id,version,started_at FROM investment_ledgers WHERE tenant_id=$1`, tenant); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id,actor_id,version,data FROM investment_portfolios WHERE tenant_id=$1 ORDER BY actor_id,id`, tenant)
	if err != nil {
		return err
	}
	type item struct {
		id, actor string
		version   int64
		p         nav.Portfolio
	}
	var items []item
	for rows.Next() {
		var v item
		var raw []byte
		if err = rows.Scan(&v.id, &v.actor, &v.version, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &v.p); err != nil {
			rows.Close()
			return d.ErrInvestmentPrecondition
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	jobs := 0
	for _, v := range items {
		p := v.p
		if p.ID != v.id || p.Version != v.version {
			return d.ErrInvestmentPrecondition
		}
		normalized, err := nav.NormalizePortfolio(nav.PortfolioInput{ID: p.ID, Name: p.Name, LedgerIDs: p.LedgerIDs, Currency: p.Currency, FXPolicyKey: p.FXPolicyKey, ConventionKey: p.ConventionKey, From: p.From, To: p.To, ExpectedVersion: p.Version, RequestKey: "archive-restore", Archived: p.Archived, AutoExtend: p.AutoExtend})
		if err != nil {
			return d.ErrInvestmentPrecondition
		}
		q := nav.Request{TenantID: tenant, UserID: v.actor, LedgerIDs: normalized.LedgerIDs, Currency: p.Currency, FXPolicyKey: p.FXPolicyKey, ConventionKey: p.ConventionKey}
		key, _, err := navSeries(q)
		if err != nil || key != p.SeriesKey {
			return d.ErrInvestmentPrecondition
		}
		if p.Archived {
			continue
		}
		var member bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 AND role IN ('owner','admin','member'))`, tenant, v.actor).Scan(&member); err != nil {
			return err
		}
		if !member {
			return d.ErrInvestmentPrecondition
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM investment_ledgers WHERE tenant_id=$1 AND id=ANY($2::uuid[])`, tenant, p.LedgerIDs).Scan(&count); err != nil {
			return err
		}
		if count != len(p.LedgerIDs) {
			return d.ErrInvestmentPrecondition
		}
		// Profiles can legitimately have been disabled after a portfolio was saved.
		// Require their definitions, but leave coverage/enabled failures to the worker.
		if _, err = readPriceConventions(ctx, tx, tenant, p.ConventionKey); err != nil {
			return err
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_fx_policies WHERE tenant_id=$1 AND policy_key=$2)`, tenant, p.FXPolicyKey).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return d.ErrInvestmentPrecondition
		}
		n, err := enqueueDailyNAV(ctx, tx, q, p.From, p.To, true)
		if err != nil {
			return err
		}
		jobs += n
		if jobs > 100000 {
			return d.ErrInvestmentLimit
		}
	}
	return nil
}
