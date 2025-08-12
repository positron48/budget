package grpcadapter

import (
	"context"
	"testing"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	useTenant "github.com/positron48/budget/internal/usecase/tenant"
)

type tRepoStub struct{ lastOwner string }

func (r *tRepoStub) Create(ctx context.Context, name, slug, defaultCurrency, ownerUserID string) (domain.Tenant, error) {
	r.lastOwner = ownerUserID
	return domain.Tenant{ID: "t1", Name: name, Slug: slug, DefaultCurrencyCode: defaultCurrency}, nil
}

func (r *tRepoStub) ListForUser(ctx context.Context, userID string) ([]domain.TenantMembership, error) {
	return []domain.TenantMembership{{Tenant: domain.Tenant{ID: "t1"}, Role: "owner", IsDefault: true}}, nil
}

// New methods to satisfy extended interface
func (r *tRepoStub) UpdateTenant(ctx context.Context, tenantID, name, slug, defaultCurrency string) (domain.Tenant, error) {
	return domain.Tenant{ID: tenantID, Name: name, Slug: slug, DefaultCurrencyCode: defaultCurrency}, nil
}

func (r *tRepoStub) ListMembers(ctx context.Context, tenantID string) ([]domain.TenantMembership, error) {
	return []domain.TenantMembership{{Tenant: domain.Tenant{ID: tenantID}, Role: domain.TenantRoleOwner, IsDefault: true}}, nil
}

func (r *tRepoStub) AddMember(ctx context.Context, tenantID, userEmail string, role domain.TenantRole) (domain.TenantMembership, error) {
	return domain.TenantMembership{Tenant: domain.Tenant{ID: tenantID}, Role: role}, nil
}

func (r *tRepoStub) UpdateMemberRole(ctx context.Context, tenantID, userID string, role domain.TenantRole) (domain.TenantMembership, error) {
	return domain.TenantMembership{Tenant: domain.Tenant{ID: tenantID}, Role: role}, nil
}
func (r *tRepoStub) RemoveMember(ctx context.Context, tenantID, userID string) error { return nil }
func (r *tRepoStub) GetUserRole(ctx context.Context, tenantID, userID string) (domain.TenantRole, error) {
	return domain.TenantRoleOwner, nil
}

func TestTenantServer_CreateAndList(t *testing.T) {
	repo := &tRepoStub{}
	svc := useTenant.NewService(repo)
	srv := NewTenantServer(svc)
	ctx := ctxutil.WithUserID(context.Background(), "u1")
	if _, err := srv.CreateTenant(ctx, &budgetv1.CreateTenantRequest{Name: "Home", Slug: "home", DefaultCurrencyCode: "USD"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if repo.lastOwner != "u1" {
		t.Fatalf("owner id not propagated: %q", repo.lastOwner)
	}
	lst, err := srv.ListMyTenants(ctx, &budgetv1.ListMyTenantsRequest{})
	if err != nil || len(lst.GetMemberships()) != 1 {
		t.Fatalf("list: %v %#v", err, lst)
	}
}
