package investment

import (
	"context"
	"strings"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

type assetActivationRepo interface {
	PreviewAssetActivation(context.Context, string, string, d.InvestmentAssetActivation) (d.InvestmentAssetActivationPreview, error)
	ActivateAsset(context.Context, string, string, d.InvestmentAssetActivation) (d.AssetAccount, error)
}

func (s *Service) AssetActivation(ctx context.Context, t, u string, m d.InvestmentAssetActivation, commit bool) (d.InvestmentAssetActivationPreview, d.AssetAccount, error) {
	var preview d.InvestmentAssetActivationPreview
	var account d.AssetAccount
	if e := s.authorize(ctx, t, u, true); e != nil {
		return preview, account, e
	}
	if !validID(m.AccountID) || !validID(m.LedgerID) || m.ValuationFrom.IsZero() || m.ValuationFrom.Before(time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)) || m.ValuationFrom.After(time.Now().UTC()) || len(m.ConventionKey) > 200 || strings.TrimSpace(m.ConventionKey) != m.ConventionKey {
		return preview, account, d.ErrInvestmentInvalid
	}
	m.ValuationFrom = m.ValuationFrom.UTC().Truncate(time.Microsecond)
	if commit && (len(m.PreviewHash) != 64 || m.ExpectedAccountVersion < 1 || m.ExpectedLedgerVersion < 1 || len(m.RequestKey) < 1 || len(m.RequestKey) > 128 || len(strings.TrimSpace(m.Evidence)) < 1 || len(m.Evidence) > 2000) {
		return preview, account, d.ErrInvestmentInvalid
	}
	repo, ok := s.repo.(assetActivationRepo)
	if !ok {
		return preview, account, d.ErrInvestmentPrecondition
	}
	if commit {
		a, e := repo.ActivateAsset(ctx, t, u, m)
		return preview, a, e
	}
	p, e := repo.PreviewAssetActivation(ctx, t, u, m)
	return p, account, e
}
