package category

import (
	"context"

	"github.com/positron48/budget/internal/domain"
)

type Repo interface {
	Create(ctx context.Context, tenantID string, kind domain.CategoryKind, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error)
	Update(ctx context.Context, id string, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error)
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (domain.Category, error)
	List(ctx context.Context, tenantID string, kind domain.CategoryKind, includeInactive bool) ([]domain.Category, error)
}

type Service struct{ repo Repo }

func NewService(repo Repo) *Service { return &Service{repo: repo} }

func (s *Service) Create(ctx context.Context, tenantID string, kind domain.CategoryKind, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) {
	return s.repo.Create(ctx, tenantID, kind, code, parentID, isActive, translations)
}

func (s *Service) Update(ctx context.Context, id string, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) {
	return s.repo.Update(ctx, id, code, parentID, isActive, translations)
}

func (s *Service) Delete(ctx context.Context, id string) error { return s.repo.Delete(ctx, id) }
func (s *Service) Get(ctx context.Context, id string) (domain.Category, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, tenantID string, kind domain.CategoryKind, includeInactive bool) ([]domain.Category, error) {
	return s.repo.List(ctx, tenantID, kind, includeInactive)
}
