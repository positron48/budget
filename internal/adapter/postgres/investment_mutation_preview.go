package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"slices"
	"sort"
	"time"
)

func (r *InvestmentRepo) PreviewMutation(ctx context.Context, t, u string, m inv.Mutation) (inv.MutationPreview, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return inv.MutationPreview{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = checkJournalReader(ctx, tx, t, u); e != nil {
		return inv.MutationPreview{}, e
	}
	out, e := r.previewMutationTx(ctx, tx, t, u, m)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func mutationPreviewHash(m inv.Mutation, base inv.PortfolioReplay, next int64) string {
	m.PreviewHash = ""
	ledgers := []d.InvestmentLedger{}
	for _, v := range base.Ledgers {
		ledgers = append(ledgers, v.Snapshot.Ledger)
	}
	sort.Slice(ledgers, func(i, j int) bool { return ledgers[i].ID < ledgers[j].ID })
	return investmentHash(struct {
		Mutation inv.Mutation
		Ledgers  []d.InvestmentLedger
		Next     int64
	}{m, ledgers, next})
}
func (r *InvestmentRepo) previewMutationTx(ctx context.Context, tx pgx.Tx, t, u string, m inv.Mutation) (inv.MutationPreview, error) {
	out := inv.MutationPreview{CheckedAt: time.Now().UTC().Truncate(time.Microsecond), Issues: []inv.MutationPreviewIssue{}, Comparisons: []inv.MutationComparison{}, AffectedLedgerIDs: []string{}}
	l, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, t, m.Operation.LedgerID))
	if e != nil {
		return out, e
	}
	ops, e := loadInvestmentOperations(ctx, tx, t, l.ID)
	if e != nil {
		return out, e
	}
	var next int64
	if e = tx.QueryRow(ctx, `SELECT next_sequence FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, t, l.ID).Scan(&next); e != nil {
		return out, e
	}
	if m.Source != nil {
		var role string
		if e = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, t, u).Scan(&role); e != nil {
			return out, e
		}
		if role != "owner" && role != "admin" {
			return out, d.ErrInvestmentForbidden
		}
		var version int64
		var used bool
		var bound string
		e = tx.QueryRow(ctx, `SELECT r.version,EXISTS(SELECT 1 FROM investment_source_links s WHERE s.tenant_id=r.tenant_id AND s.import_id=r.import_id AND s.record=r.record),COALESCE((SELECT b.ledger_id::text FROM investment_import_ledger_bindings b WHERE b.tenant_id=r.tenant_id AND b.import_id=r.import_id AND b.account_id=r.account_id),'') FROM investment_import_rows r WHERE r.tenant_id=$1 AND r.import_id=$2 AND r.record=$3`, t, m.Source.ImportID, m.Source.Record).Scan(&version, &used, &bound)
		if e != nil {
			return out, investmentError(e)
		}
		if used || version != m.Source.ExpectedVersion || bound != "" && bound != l.ID {
			return out, d.ErrInvestmentConflict
		}
	}
	if m.Action != "void" {
		if e := verifyOperationLotConversion(ctx, tx, t, m.Operation, false); e != nil {
			return out, e
		}
	}
	p, proposed, _, dirty, e := inv.ProposeMutation(l, ops, m, uuid.NewSHA1(uuid.NameSpaceOID, []byte(t+":"+u+":"+m.RequestKey)).String(), next, out.CheckedAt, false)
	if e != nil {
		return out, e
	}
	out.Proposed = p
	out.DirtyFrom = dirty
	base, e := readConnectedInvestmentReplay(ctx, tx, t, []string{l.ID}, nil, out.CheckedAt, nil)
	if e != nil {
		return out, e
	}
	for id := range base.Ledgers {
		out.AffectedLedgerIDs = append(out.AffectedLedgerIDs, id)
	}
	slices.Sort(out.AffectedLedgerIDs)
	after, e := readConnectedInvestmentReplay(ctx, tx, t, []string{l.ID}, map[string]investmentReplayInput{l.ID: {l, proposed}}, out.CheckedAt, nil)
	if e != nil {
		var cash *inv.ReplayCashError
		var op *inv.ReplayOperationError
		switch {
		case errors.As(e, &cash):
			out.Issues = append(out.Issues, inv.MutationPreviewIssue{Code: "negative_cash", At: cash.At, Cash: &cash.Balance})
		case errors.As(e, &op):
			out.Issues = append(out.Issues, inv.MutationPreviewIssue{Code: "operation_replay_failed", OperationID: op.OperationID, InstrumentKey: op.InstrumentKey})
		case errors.Is(e, d.ErrInvestmentInvalid), errors.Is(e, d.ErrInvestmentPrecondition):
			out.Issues = append(out.Issues, inv.MutationPreviewIssue{Code: "history_inconsistent"})
		default:
			return out, e
		}
		return out, nil
	}
	points := []struct {
		stage string
		at    time.Time
	}{{"operation", p.OccurredAt}, {"settlement", p.SettlementAt}, {"current", out.CheckedAt}}
	if f := p.TradeFinancials; f != nil {
		for _, m := range []d.InvestmentTradeMoney{f.Fee, f.Tax} {
			if m.Amount != "0" && !m.At.Equal(p.SettlementAt) {
				points = append(points, struct {
					stage string
					at    time.Time
				}{"component", m.At})
			}
		}
	}
	for _, point := range points {
		beforeAt, afterAt := base, after
		if point.stage != "current" {
			beforeAt, e = readConnectedInvestmentReplay(ctx, tx, t, []string{l.ID}, nil, point.at, nil)
			if e != nil {
				return out, e
			}
			afterAt, e = readConnectedInvestmentReplay(ctx, tx, t, []string{l.ID}, map[string]investmentReplayInput{l.ID: {l, proposed}}, point.at, nil)
			if e != nil {
				return out, e
			}
		}
		for _, id := range out.AffectedLedgerIDs {
			out.Comparisons = append(out.Comparisons, inv.CompleteMutationComparison(inv.MutationComparison{Stage: point.stage, At: point.at, Before: beforeAt.Ledgers[id].Snapshot, After: afterAt.Ledgers[id].Snapshot}))
		}
	}
	out.Valid = true
	out.Hash = mutationPreviewHash(m, base, next)
	return out, nil
}
