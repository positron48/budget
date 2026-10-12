package investment

import (
	"context"
	d "github.com/positron48/budget/internal/domain"
	"time"
)

type TransferPreview struct {
	Transfer d.InvestmentTransfer
	Preview  MutationPreview
}
type AssetTransferComparison struct {
	Stage, AccountID, Currency string
	At                         time.Time
	BeforeKnown, AfterKnown    bool
	BeforeMinor, AfterMinor    int64
}
type CashTransferPreview struct {
	Transfer CashTransfer
	Preview  MutationPreview
	Accounts []AssetTransferComparison
}
type transferPreviewRepo interface {
	PreviewTransfer(context.Context, string, string, TransferMutation) (TransferPreview, error)
	PreviewCashTransfer(context.Context, string, string, CashTransferMutation) (CashTransferPreview, error)
}

func (s *Service) PreviewTransfer(ctx context.Context, t, u string, m TransferMutation) (TransferPreview, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return TransferPreview{}, e
	}
	m.PreviewHash = ""
	m, e := NormalizeTransferMutation(t, u, m, time.Now().UTC())
	if e != nil {
		return TransferPreview{}, e
	}
	repo, ok := s.repo.(transferPreviewRepo)
	if !ok {
		return TransferPreview{}, d.ErrInvestmentPrecondition
	}
	return repo.PreviewTransfer(ctx, t, u, m)
}
func (s *Service) PreviewCashTransfer(ctx context.Context, t, u string, m CashTransferMutation) (CashTransferPreview, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return CashTransferPreview{}, e
	}
	m.PreviewHash = ""
	m, e := NormalizeCashTransferMutation(t, u, m, time.Now().UTC())
	if e != nil {
		return CashTransferPreview{}, e
	}
	repo, ok := s.repo.(transferPreviewRepo)
	if !ok {
		return CashTransferPreview{}, d.ErrInvestmentPrecondition
	}
	return repo.PreviewCashTransfer(ctx, t, u, m)
}
