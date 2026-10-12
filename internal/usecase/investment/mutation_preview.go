package investment

import (
	"context"
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"slices"
	"time"
)

type MutationPreviewIssue struct {
	Code, OperationID, InstrumentKey string
	At                               time.Time
	Cash                             *d.InvestmentCash
}
type MutationComparison struct {
	Stage          string
	At             time.Time
	Before, After  d.InvestmentSnapshot
	CashChange     []d.InvestmentCash
	PositionChange []d.InvestmentPosition
}
type MutationPreview struct {
	Valid                bool
	Hash                 string
	CheckedAt, DirtyFrom time.Time
	Issues               []MutationPreviewIssue
	Comparisons          []MutationComparison
	AffectedLedgerIDs    []string
	Proposed             d.InvestmentOperation
}
type mutationPreviewRepo interface {
	PreviewMutation(context.Context, string, string, Mutation) (MutationPreview, error)
}

func (s *Service) PreviewMutation(ctx context.Context, t, u string, m Mutation) (MutationPreview, error) {
	if e := s.authorize(ctx, t, u, m.Source != nil); e != nil {
		return MutationPreview{}, e
	}
	m.PreviewHash = ""
	m, e := NormalizeMutation(t, u, m, time.Now().UTC())
	if e != nil {
		return MutationPreview{}, e
	}
	repo, ok := s.repo.(mutationPreviewRepo)
	if !ok {
		return MutationPreview{}, d.ErrInvestmentPrecondition
	}
	return repo.PreviewMutation(ctx, t, u, m)
}

// ProposeMutation is shared by the read-only preview and the financial writer.
// It preserves server-owned provenance and ordering, and returns a cloned history.
func ProposeMutation(l d.InvestmentLedger, input []d.InvestmentOperation, m Mutation, newID string, sequence int64, now time.Time, transfer bool) (d.InvestmentOperation, []d.InvestmentOperation, *d.InvestmentOperation, time.Time, error) {
	empty := d.InvestmentOperation{}
	fail := func(e error) (d.InvestmentOperation, []d.InvestmentOperation, *d.InvestmentOperation, time.Time, error) {
		return empty, nil, nil, time.Time{}, e
	}
	if l.Archived {
		return fail(d.ErrInvestmentPrecondition)
	}
	if l.Version != m.ExpectedLedgerVersion {
		return fail(d.ErrInvestmentConflict)
	}
	ops := slices.Clone(input)
	p := m.Operation
	dirty := p.OccurredAt
	if m.Action == "create" {
		p.Source = m.Source
		p.ID = newID
		p.Version = 1
		p.Status = "posted"
		p.CreatedAt = now
		p.Sequence = sequence
		ops = append(ops, p)
		return p, ops, nil, dirty, nil
	}
	for i, old := range ops {
		if old.ID != p.ID {
			continue
		}
		if (old.TransferID != "" || old.BudgetTransferID != "") && !transfer {
			return fail(d.ErrInvestmentPrecondition)
		}
		if old.Version != m.ExpectedOperationVersion || old.Status != "posted" {
			return fail(d.ErrInvestmentConflict)
		}
		dirty = old.OccurredAt
		if m.Action == "void" {
			p = old
			p.Status = "voided"
			p.ActorID = m.Operation.ActorID
		} else {
			p.Source = old.Source
			p.Sequence = old.Sequence
			p.CreatedAt = old.CreatedAt
			p.Status = "posted"
			if p.OccurredAt.Before(dirty) {
				dirty = p.OccurredAt
			}
		}
		p.Version = old.Version + 1
		ops[i] = p
		return p, ops, &old, dirty, nil
	}
	return fail(d.ErrInvestmentNotFound)
}

func CompleteMutationComparison(c MutationComparison) MutationComparison {
	cash := map[string]bool{}
	b := map[string]d.InvestmentCash{}
	a := map[string]d.InvestmentCash{}
	for _, v := range c.Before.Cash {
		cash[v.Currency] = true
		b[v.Currency] = v
	}
	for _, v := range c.After.Cash {
		cash[v.Currency] = true
		a[v.Currency] = v
	}
	keys := []string{}
	for k := range cash {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	diff := func(x, y string) string {
		if x == "" {
			x = "0"
		}
		if y == "" {
			y = "0"
		}
		return decimalString(new(big.Rat).Sub(rational(x), rational(y)))
	}
	for _, k := range keys {
		x, y := a[k], b[k]
		c.CashChange = append(c.CashChange, d.InvestmentCash{Currency: k, Settled: diff(x.Settled, y.Settled), Receivable: diff(x.Receivable, y.Receivable), Payable: diff(x.Payable, y.Payable), Economic: diff(x.Economic, y.Economic)})
	}
	bp := map[string]d.InvestmentPosition{}
	ap := map[string]d.InvestmentPosition{}
	keys = nil
	seen := map[string]bool{}
	for _, v := range c.Before.Positions {
		bp[v.InstrumentKey] = v
		seen[v.InstrumentKey] = true
	}
	for _, v := range c.After.Positions {
		ap[v.InstrumentKey] = v
		seen[v.InstrumentKey] = true
	}
	for k := range seen {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		x, y := ap[k], bp[k]
		v := d.InvestmentPosition{InstrumentKey: k, Currency: x.Currency, Quantity: diff(x.Quantity, y.Quantity)}
		if v.Currency == "" {
			v.Currency = y.Currency
		}
		if (x.InstrumentKey == "" || y.InstrumentKey == "" || x.Currency == y.Currency) && (x.InstrumentKey == "" || x.CleanCost != "") && (y.InstrumentKey == "" || y.CleanCost != "") {
			v.CleanCost = diff(x.CleanCost, y.CleanCost)
		}
		c.PositionChange = append(c.PositionChange, v)
	}
	return c
}
