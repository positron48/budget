package tenant

import (
	"context"
	"testing"

	"github.com/positron48/budget/internal/domain"
)

type stubRepo struct{}

func (stubRepo) Create(ctx context.Context, name, slug, defaultCurrency, ownerUserID string) (domain.Tenant, error) {
	return domain.Tenant{ID: "t1", Name: name, Slug: slug, DefaultCurrencyCode: defaultCurrency}, nil
}

func (stubRepo) ListForUser(ctx context.Context, userID string) ([]domain.TenantMembership, error) {
	return []domain.TenantMembership{{Tenant: domain.Tenant{ID: "t1"}, Role: "owner", IsDefault: true}}, nil
}

// New interface methods
func (stubRepo) UpdateTenant(ctx context.Context, tenantID, name, slug, defaultCurrency string) (domain.Tenant, error) {
	return domain.Tenant{ID: tenantID, Name: name, Slug: slug, DefaultCurrencyCode: defaultCurrency}, nil
}

func (stubRepo) ListMembers(ctx context.Context, tenantID string) ([]domain.TenantMembership, error) {
	return []domain.TenantMembership{{Tenant: domain.Tenant{ID: tenantID}, Role: domain.TenantRoleOwner}}, nil
}

func (stubRepo) AddMember(ctx context.Context, tenantID, userEmail string, role domain.TenantRole) (domain.TenantMembership, error) {
	return domain.TenantMembership{Tenant: domain.Tenant{ID: tenantID}, Role: role}, nil
}

func (stubRepo) UpdateMemberRole(ctx context.Context, tenantID, userID string, role domain.TenantRole) (domain.TenantMembership, error) {
	return domain.TenantMembership{Tenant: domain.Tenant{ID: tenantID}, Role: role}, nil
}
func (stubRepo) RemoveMember(ctx context.Context, tenantID, userID string) error { return nil }
func (stubRepo) GetUserRole(ctx context.Context, tenantID, userID string) (domain.TenantRole, error) {
	return domain.TenantRoleOwner, nil
}

func TestService_CreateAndList(t *testing.T) {
	svc := NewService(stubRepo{})
	ctx := context.Background()
	tnt, err := svc.CreateTenant(ctx, "Home", "home", "USD", "u1")
	if err != nil || tnt.ID == "" {
		t.Fatalf("create: %v %#v", err, tnt)
	}
	lst, err := svc.ListMyTenants(ctx, "u1")
	if err != nil || len(lst) != 1 || !lst[0].IsDefault {
		t.Fatalf("list: %v %#v", err, lst)
	}
}
