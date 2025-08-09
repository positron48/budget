package tenant

import (
	"context"

	"github.com/positron48/budget/internal/domain"
)

type Repo interface {
	Create(ctx context.Context, name, slug, defaultCurrency string, ownerUserID string) (domain.Tenant, error)
	ListForUser(ctx context.Context, userID string) ([]domain.TenantMembership, error)
}

type Service struct{ repo Repo }

func NewService(repo Repo) *Service { return &Service{repo: repo} }

func (s *Service) CreateTenant(ctx context.Context, name, slug, defaultCurrency, ownerUserID string) (domain.Tenant, error) {
	return s.repo.Create(ctx, name, slug, defaultCurrency, ownerUserID)
}

func (s *Service) ListMyTenants(ctx context.Context, userID string) ([]domain.TenantMembership, error) {
	return s.repo.ListForUser(ctx, userID)
}
