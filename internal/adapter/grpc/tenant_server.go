//go:build !ignore
// +build !ignore

package grpcadapter

import (
	"context"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/tenant"
)

type TenantServer struct {
	budgetv1.UnimplementedTenantServiceServer
	svc *tenant.Service
}

func NewTenantServer(svc *tenant.Service) *TenantServer { return &TenantServer{svc: svc} }

func (s *TenantServer) CreateTenant(ctx context.Context, req *budgetv1.CreateTenantRequest) (*budgetv1.CreateTenantResponse, error) {
	t, err := s.svc.CreateTenant(ctx, req.GetName(), req.GetSlug(), req.GetDefaultCurrencyCode(), ctxUserID(ctx))
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.CreateTenantResponse{Tenant: &budgetv1.Tenant{Id: t.ID, Name: t.Name, Slug: t.Slug, DefaultCurrencyCode: t.DefaultCurrencyCode}}, nil
}

func (s *TenantServer) ListMyTenants(ctx context.Context, _ *budgetv1.ListMyTenantsRequest) (*budgetv1.ListMyTenantsResponse, error) {
	ms, err := s.svc.ListMyTenants(ctx, ctxUserID(ctx))
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]*budgetv1.TenantMembership, 0, len(ms))
	for _, m := range ms {
		out = append(out, &budgetv1.TenantMembership{Tenant: &budgetv1.Tenant{Id: m.Tenant.ID, Name: m.Tenant.Name, Slug: m.Tenant.Slug, DefaultCurrencyCode: m.Tenant.DefaultCurrencyCode}, Role: mapRole(string(m.Role)), IsDefault: m.IsDefault})
	}
	return &budgetv1.ListMyTenantsResponse{Memberships: out}, nil
}

func (s *TenantServer) UpdateTenant(ctx context.Context, req *budgetv1.UpdateTenantRequest) (*budgetv1.UpdateTenantResponse, error) {
	t, err := s.svc.UpdateTenant(ctx, ctxUserID(ctx), req.GetId(), req.GetName(), req.GetSlug(), req.GetDefaultCurrencyCode())
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.UpdateTenantResponse{Tenant: &budgetv1.Tenant{Id: t.ID, Name: t.Name, Slug: t.Slug, DefaultCurrencyCode: t.DefaultCurrencyCode}}, nil
}

func (s *TenantServer) ListMembers(ctx context.Context, req *budgetv1.ListMembersRequest) (*budgetv1.ListMembersResponse, error) {
	ms, err := s.svc.ListMembers(ctx, ctxUserID(ctx), req.GetTenantId())
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]*budgetv1.TenantMember, 0, len(ms))
	for _, m := range ms {
		out = append(out, &budgetv1.TenantMember{
			User:      &budgetv1.User{Id: m.UserID, Email: m.UserEmail, Name: m.UserName},
			Role:      mapRole(string(m.Role)),
			IsDefault: m.IsDefault,
		})
	}
	return &budgetv1.ListMembersResponse{Members: out}, nil
}

func (s *TenantServer) AddMember(ctx context.Context, req *budgetv1.AddMemberRequest) (*budgetv1.AddMemberResponse, error) {
	m, err := s.svc.AddMember(ctx, ctxUserID(ctx), req.GetTenantId(), req.GetEmail(), tenantRoleFromPb(req.GetRole()))
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.AddMemberResponse{Member: &budgetv1.TenantMember{User: &budgetv1.User{Id: m.UserID, Email: m.UserEmail, Name: m.UserName}, Role: mapRole(string(m.Role)), IsDefault: m.IsDefault}}, nil
}

func (s *TenantServer) UpdateMemberRole(ctx context.Context, req *budgetv1.UpdateMemberRoleRequest) (*budgetv1.UpdateMemberRoleResponse, error) {
	m, err := s.svc.UpdateMemberRole(ctx, ctxUserID(ctx), req.GetTenantId(), req.GetUserId(), tenantRoleFromPb(req.GetRole()))
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.UpdateMemberRoleResponse{Member: &budgetv1.TenantMember{User: &budgetv1.User{Id: m.UserID, Email: m.UserEmail, Name: m.UserName}, Role: mapRole(string(m.Role)), IsDefault: m.IsDefault}}, nil
}

func (s *TenantServer) RemoveMember(ctx context.Context, req *budgetv1.RemoveMemberRequest) (*budgetv1.RemoveMemberResponse, error) {
	if err := s.svc.RemoveMember(ctx, ctxUserID(ctx), req.GetTenantId(), req.GetUserId()); err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.RemoveMemberResponse{}, nil
}

func tenantRoleFromPb(r budgetv1.TenantRole) domain.TenantRole {
	switch r {
	case budgetv1.TenantRole_TENANT_ROLE_OWNER:
		return domain.TenantRoleOwner
	case budgetv1.TenantRole_TENANT_ROLE_ADMIN:
		return domain.TenantRoleAdmin
	case budgetv1.TenantRole_TENANT_ROLE_MEMBER:
		return domain.TenantRoleMember
	default:
		return ""
	}
}

// ctxUserID – заглушка для извлечения user_id из контекста (интерсептор аутентификации)
func ctxUserID(ctx context.Context) string {
	if v, ok := ctxutil.UserIDFromContext(ctx); ok {
		return v
	}
	return ""
}
