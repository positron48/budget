//go:build !ignore
// +build !ignore

package grpcadapter

import (
	"context"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
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
		return nil, err
	}
	return &budgetv1.CreateTenantResponse{Tenant: &budgetv1.Tenant{Id: t.ID, Name: t.Name, Slug: t.Slug, DefaultCurrencyCode: t.DefaultCurrencyCode}}, nil
}

func (s *TenantServer) ListMyTenants(ctx context.Context, _ *budgetv1.ListMyTenantsRequest) (*budgetv1.ListMyTenantsResponse, error) {
	ms, err := s.svc.ListMyTenants(ctx, ctxUserID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*budgetv1.TenantMembership, 0, len(ms))
	for _, m := range ms {
		out = append(out, &budgetv1.TenantMembership{Tenant: &budgetv1.Tenant{Id: m.Tenant.ID, Name: m.Tenant.Name, Slug: m.Tenant.Slug, DefaultCurrencyCode: m.Tenant.DefaultCurrencyCode}, Role: mapRole(string(m.Role)), IsDefault: m.IsDefault})
	}
	return &budgetv1.ListMyTenantsResponse{Memberships: out}, nil
}

// ctxUserID – заглушка для извлечения user_id из контекста (интерсептор аутентификации)
func ctxUserID(_ context.Context) string { return "" }
