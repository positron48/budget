package tenant

import (
	"context"
	"errors"

	"github.com/positron48/budget/internal/domain"
)

var (
	ErrPermissionDenied = errors.New("permission denied")
	ErrAlreadyMember    = errors.New("already_member")
)

type Repo interface {
	Create(ctx context.Context, name, slug, defaultCurrency string, ownerUserID string) (domain.Tenant, error)
	ListForUser(ctx context.Context, userID string) ([]domain.TenantMembership, error)

	// New repo methods
	UpdateTenant(ctx context.Context, tenantID, name, slug, defaultCurrency string) (domain.Tenant, error)
	ListMembers(ctx context.Context, tenantID string) ([]domain.TenantMembership, error)
	AddMember(ctx context.Context, tenantID, userEmail string, role domain.TenantRole) (domain.TenantMembership, error)
	UpdateMemberRole(ctx context.Context, tenantID, userID string, role domain.TenantRole) (domain.TenantMembership, error)
	RemoveMember(ctx context.Context, tenantID, userID string) error
	GetUserRole(ctx context.Context, tenantID, userID string) (domain.TenantRole, error)
}

type Service struct{ repo Repo }

func NewService(repo Repo) *Service { return &Service{repo: repo} }

func (s *Service) CreateTenant(ctx context.Context, name, slug, defaultCurrency, ownerUserID string) (domain.Tenant, error) {
	return s.repo.Create(ctx, name, slug, defaultCurrency, ownerUserID)
}

func (s *Service) ListMyTenants(ctx context.Context, userID string) ([]domain.TenantMembership, error) {
	return s.repo.ListForUser(ctx, userID)
}

// Permissions: only owner or admin can update tenant
func (s *Service) UpdateTenant(ctx context.Context, actingUserID, tenantID, name, slug, defaultCurrency string) (domain.Tenant, error) {
	role, err := s.repo.GetUserRole(ctx, tenantID, actingUserID)
	if err != nil {
		return domain.Tenant{}, err
	}
	if role != domain.TenantRoleOwner && role != domain.TenantRoleAdmin {
		return domain.Tenant{}, ErrPermissionDenied
	}
	return s.repo.UpdateTenant(ctx, tenantID, name, slug, defaultCurrency)
}

// Permissions: list members - any member can view
func (s *Service) ListMembers(ctx context.Context, actingUserID, tenantID string) ([]domain.TenantMembership, error) {
	role, err := s.repo.GetUserRole(ctx, tenantID, actingUserID)
	if err != nil {
		return nil, err
	}
	if role == "" { // not a member
		return nil, ErrPermissionDenied
	}
	return s.repo.ListMembers(ctx, tenantID)
}

// Permissions: add member - owner or admin
func (s *Service) AddMember(ctx context.Context, actingUserID, tenantID, userEmail string, role domain.TenantRole) (domain.TenantMembership, error) {
	ar, err := s.repo.GetUserRole(ctx, tenantID, actingUserID)
	if err != nil {
		return domain.TenantMembership{}, err
	}
	if ar != domain.TenantRoleOwner && ar != domain.TenantRoleAdmin {
		return domain.TenantMembership{}, ErrPermissionDenied
	}
	if role == domain.TenantRoleOwner && ar != domain.TenantRoleOwner {
		return domain.TenantMembership{}, ErrPermissionDenied
	}
	return s.repo.AddMember(ctx, tenantID, userEmail, role)
}

// Permissions: update member role - owner or admin, but only owner can grant owner
func (s *Service) UpdateMemberRole(ctx context.Context, actingUserID, tenantID, userID string, role domain.TenantRole) (domain.TenantMembership, error) {
	ar, err := s.repo.GetUserRole(ctx, tenantID, actingUserID)
	if err != nil {
		return domain.TenantMembership{}, err
	}
	if ar != domain.TenantRoleOwner && ar != domain.TenantRoleAdmin {
		return domain.TenantMembership{}, ErrPermissionDenied
	}
	if role == domain.TenantRoleOwner && ar != domain.TenantRoleOwner {
		return domain.TenantMembership{}, ErrPermissionDenied
	}
	// Prevent self-demotion: owner must not downgrade own role
	if actingUserID == userID && role != domain.TenantRoleOwner {
		return domain.TenantMembership{}, ErrPermissionDenied
	}
	return s.repo.UpdateMemberRole(ctx, tenantID, userID, role)
}

// Permissions: remove member - owner or admin; only owner can remove another owner
func (s *Service) RemoveMember(ctx context.Context, actingUserID, tenantID, userID string) error {
	ar, err := s.repo.GetUserRole(ctx, tenantID, actingUserID)
	if err != nil {
		return err
	}
	if ar != domain.TenantRoleOwner && ar != domain.TenantRoleAdmin {
		return ErrPermissionDenied
	}
	// Prevent removing yourself from the account
	if actingUserID == userID {
		return ErrPermissionDenied
	}
	tr, err := s.repo.GetUserRole(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	if tr == domain.TenantRoleOwner && ar != domain.TenantRoleOwner {
		return ErrPermissionDenied
	}
	return s.repo.RemoveMember(ctx, tenantID, userID)
}
