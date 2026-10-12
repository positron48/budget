package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"slices"
	"time"
)

type linkedPreviewPlan struct {
	proposed   d.InvestmentOperation
	overrides  map[string]investmentReplayInput
	next       map[string]int64
	mutations  []inv.Mutation
	dirty      time.Time
	previousAt time.Time
}

func linkedPreviewID(t, u, key, part string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(t+":"+u+":"+key+":"+part)).String()
}
func (p *linkedPreviewPlan) add(ctx context.Context, tx pgx.Tx, t string, m inv.Mutation) error {
	l, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, t, m.Operation.LedgerID))
	if e != nil {
		return e
	}
	ops, e := loadInvestmentOperations(ctx, tx, t, l.ID)
	if e != nil {
		return e
	}
	var next int64
	if e = tx.QueryRow(ctx, `SELECT next_sequence FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, t, l.ID).Scan(&next); e != nil {
		return e
	}
	proposed, ops, before, dirty, e := inv.ProposeMutation(l, ops, m, m.Operation.ID, next, time.Now().UTC().Truncate(time.Microsecond), true)
	if e != nil {
		return e
	}
	// Keep old/new dates, immutable pair IDs and versions identical in the writer.
	if before != nil {
		p.previousAt = before.OccurredAt
	}
	m.Operation.ID = proposed.ID
	if len(p.mutations) == 0 {
		p.proposed = proposed
	}
	p.mutations = append(p.mutations, m)
	p.next[l.ID] = next
	p.overrides[l.ID] = investmentReplayInput{l, ops}
	if p.dirty.IsZero() || dirty.Before(p.dirty) {
		p.dirty = dirty
	}
	return nil
}
func linkedReplayIssue(e error) (inv.MutationPreviewIssue, bool) {
	var cash *inv.ReplayCashError
	var op *inv.ReplayOperationError
	switch {
	case errors.As(e, &cash):
		return inv.MutationPreviewIssue{Code: "negative_cash", At: cash.At, Cash: &cash.Balance}, true
	case errors.As(e, &op):
		return inv.MutationPreviewIssue{Code: "operation_replay_failed", OperationID: op.OperationID, InstrumentKey: op.InstrumentKey}, true
	case errors.Is(e, d.ErrInvestmentInvalid), errors.Is(e, d.ErrInvestmentPrecondition):
		return inv.MutationPreviewIssue{Code: "history_inconsistent"}, true
	}
	return inv.MutationPreviewIssue{}, false
}
func (r *InvestmentRepo) linkedFinancialPreview(ctx context.Context, tx pgx.Tx, t, u string, command any, p linkedPreviewPlan) (inv.MutationPreview, error) {
	out := inv.MutationPreview{CheckedAt: time.Now().UTC().Truncate(time.Microsecond), DirtyFrom: p.dirty, Comparisons: []inv.MutationComparison{}, Issues: []inv.MutationPreviewIssue{}}
	seeds := []string{}
	for id := range p.overrides {
		seeds = append(seeds, id)
	}
	slices.Sort(seeds)
	base, e := readConnectedInvestmentReplay(ctx, tx, t, seeds, nil, out.CheckedAt, nil)
	if e != nil {
		return out, e
	}
	for id := range base.Ledgers {
		out.AffectedLedgerIDs = append(out.AffectedLedgerIDs, id)
	}
	slices.Sort(out.AffectedLedgerIDs)
	_, e = readConnectedInvestmentReplay(ctx, tx, t, seeds, p.overrides, out.CheckedAt, nil)
	if e != nil {
		if issue, ok := linkedReplayIssue(e); ok {
			out.Issues = append(out.Issues, issue)
			return out, nil
		}
		return out, e
	}
	points := []struct {
		stage string
		at    time.Time
	}{}
	if !p.previousAt.IsZero() {
		points = append(points, struct {
			stage string
			at    time.Time
		}{"previous_operation", p.previousAt})
	}
	for _, m := range p.mutations {
		if !m.Operation.OccurredAt.IsZero() {
			points = append(points, struct {
				stage string
				at    time.Time
			}{"operation", m.Operation.OccurredAt})
		}
	}
	points = append(points, struct {
		stage string
		at    time.Time
	}{"current", out.CheckedAt})
	seen := map[string]bool{}
	for _, point := range points {
		key := point.stage + point.at.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		before, e := readConnectedInvestmentReplay(ctx, tx, t, seeds, nil, point.at, nil)
		if e != nil {
			return out, e
		}
		after, e := readConnectedInvestmentReplay(ctx, tx, t, seeds, p.overrides, point.at, nil)
		if e != nil {
			return out, e
		}
		for _, id := range out.AffectedLedgerIDs {
			out.Comparisons = append(out.Comparisons, inv.CompleteMutationComparison(inv.MutationComparison{Stage: point.stage, At: point.at, Before: before.Ledgers[id].Snapshot, After: after.Ledgers[id].Snapshot}))
		}
	}
	ledgers := []d.InvestmentLedger{}
	for _, id := range out.AffectedLedgerIDs {
		ledgers = append(ledgers, base.Ledgers[id].Snapshot.Ledger)
	}
	out.Valid = true
	out.Hash = investmentHash(struct {
		Tenant, Actor string
		Command       any
		Ledgers       []d.InvestmentLedger
		Next          map[string]int64
	}{t, u, command, ledgers, p.next})
	out.Proposed = p.proposed
	return out, nil
}
func (r *InvestmentRepo) PreviewTransfer(ctx context.Context, t, u string, m inv.TransferMutation) (inv.TransferPreview, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return inv.TransferPreview{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = checkJournalReader(ctx, tx, t, u); e != nil {
		return inv.TransferPreview{}, e
	}
	transfer, p, e := r.prepareTransferTx(ctx, tx, t, u, m)
	if e != nil {
		return inv.TransferPreview{}, e
	}
	out, e := r.transferPreviewTx(ctx, tx, t, u, m, transfer, p)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (r *InvestmentRepo) transferPreviewTx(ctx context.Context, tx pgx.Tx, t, u string, m inv.TransferMutation, transfer d.InvestmentTransfer, p linkedPreviewPlan) (inv.TransferPreview, error) {
	m.PreviewHash = ""
	preview, e := r.linkedFinancialPreview(ctx, tx, t, u, m, p)
	return inv.TransferPreview{Transfer: transfer, Preview: preview}, e
}
