package postgres

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

type investmentReplayInput struct {
	Ledger     d.InvestmentLedger
	Operations []d.InvestmentOperation
}

// The component includes tombstoned links: voiding a pair must also rebuild the
// account it disconnects. Reads stay in the caller's repeatable-read snapshot;
// writes hold the common tenant financial lock before reaching this helper.
func readConnectedInvestmentReplay(ctx context.Context, tx pgx.Tx, tenant string, seeds []string, overrides map[string]investmentReplayInput, at time.Time, origins []string) (inv.PortfolioReplay, error) {
	empty := inv.PortfolioReplay{}
	seeds = slices.Clone(seeds)
	for id := range overrides {
		if !slices.Contains(seeds, id) {
			seeds = append(seeds, id)
		}
	}
	rows, e := tx.Query(ctx, `WITH RECURSIVE connected(id) AS (
 SELECT unnest($2::uuid[])
 UNION
 SELECT CASE WHEN o.ledger_id=c.id THEN (o.data->>'CounterpartyLedgerID')::uuid ELSE o.ledger_id END
 FROM connected c JOIN investment_operations o ON (o.ledger_id=c.id OR o.data->>'CounterpartyLedgerID'=c.id::text)
 WHERE o.tenant_id=$1 AND o.kind IN ('security_transfer_in','security_transfer_out')
 ) SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id IN (SELECT id FROM connected) ORDER BY id LIMIT 101`, tenant, seeds)
	if e != nil {
		return empty, e
	}
	ledgers := []d.InvestmentLedger{}
	for rows.Next() {
		l, e := scanLedger(rows)
		if e != nil {
			rows.Close()
			return empty, e
		}
		ledgers = append(ledgers, l)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return empty, e
	}
	if len(ledgers) > 100 {
		return empty, d.ErrInvestmentLimit
	}
	found := map[string]bool{}
	operations := []d.InvestmentOperation{}
	for i, l := range ledgers {
		found[l.ID] = true
		input, ok := overrides[l.ID]
		if ok {
			if input.Ledger.ID != l.ID || input.Ledger.TenantID != tenant {
				return empty, d.ErrInvestmentInvalid
			}
			ledgers[i] = input.Ledger
		} else {
			input.Operations, e = loadInvestmentOperations(ctx, tx, tenant, l.ID)
			if e != nil {
				return empty, e
			}
		}
		operations = append(operations, input.Operations...)
		if len(operations) > 100000 {
			return empty, d.ErrInvestmentLimit
		}
	}
	for _, id := range seeds {
		if !found[id] {
			return empty, d.ErrInvestmentNotFound
		}
	}
	if len(origins) > 0 {
		return inv.ReplayPortfolioLotHistory(ledgers, operations, at, origins)
	}
	return inv.ReplayPortfolio(ledgers, operations, at)
}
func replayInvestmentLedgerTx(ctx context.Context, tx pgx.Tx, tenant string, ledger d.InvestmentLedger, ops []d.InvestmentOperation, at time.Time) (inv.ReplayResult, error) {
	if at.Before(ledger.StartedAt) {
		return inv.Replay(ledger, ops, at)
	}
	out, e := readConnectedInvestmentReplay(ctx, tx, tenant, []string{ledger.ID}, map[string]investmentReplayInput{ledger.ID: {ledger, ops}}, at, nil)
	return out.Ledgers[ledger.ID], e
}

// Journal versions count financial revisions only. A source-cost correction
// rebuilds dependent projections under their unchanged native revision; the
// tenant valuation generation fences derived reads and cursors independently.
func replaceConnectedInvestmentDependents(ctx context.Context, tx pgx.Tx, tenant string, replay inv.PortfolioReplay, skip map[string]bool, dirty time.Time) error {
	ids := []string{}
	for id := range replay.Ledgers {
		if !skip[id] {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		result := replay.Ledgers[id]
		if e := replaceInvestmentLegs(ctx, tx, tenant, id, result); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO investment_recalculation_jobs(tenant_id,ledger_id,journal_revision,dirty_from) VALUES($1,$2,$3,$4)
 ON CONFLICT(tenant_id,ledger_id,journal_revision) DO UPDATE SET dirty_from=LEAST(investment_recalculation_jobs.dirty_from,EXCLUDED.dirty_from),status='pending'`, tenant, id, result.Snapshot.Ledger.Version, dirty); e != nil {
			return e
		}
	}
	return nil
}
func investmentGenerationTx(ctx context.Context, tx pgx.Tx, tenant string) (int64, error) {
	var generation int64
	e := tx.QueryRow(ctx, `SELECT COALESCE((SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1),0)`, tenant).Scan(&generation)
	return generation, e
}
