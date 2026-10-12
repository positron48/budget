package investmentimport

import (
	"context"
	"errors"
	d "github.com/positron48/budget/internal/domain"
	"testing"
)

type brokerServiceRepo struct {
	ImportRepo
	calls        int
	tenant, user string
	raw          []byte
}

func (r *brokerServiceRepo) SaveBrokerSnapshot(_ context.Context, tenant, user string, raw []byte) (string, error) {
	r.calls++
	r.tenant = tenant
	r.user = user
	r.raw = raw
	return "saved", nil
}
func (r *brokerServiceRepo) BrokerSnapshotBytes(_ context.Context, tenant, id string) ([]byte, error) {
	r.calls++
	r.tenant = tenant
	return r.raw, nil
}
func TestBrokerSourceAuthorizationAndScope(t *testing.T) {
	const tenant = "00000000-0000-0000-0000-000000000001"
	const user = "00000000-0000-0000-0000-000000000002"
	raw := brokerFixture(t, "["+sourceBrokerOp+"]")
	for _, role := range []d.TenantRole{"", d.TenantRoleMember, d.TenantRoleAdmin, d.TenantRoleOwner} {
		repo := &brokerServiceRepo{raw: raw}
		svc := NewImportService(repo, testImportRoles{role})
		_, err := svc.UploadBrokerSnapshot(context.Background(), tenant, user, raw)
		allowed := role == d.TenantRoleAdmin || role == d.TenantRoleOwner
		if allowed && (err != nil || repo.calls != 1 || repo.tenant != tenant || repo.user != user) {
			t.Fatal(role, repo, err)
		}
		if !allowed && (!errors.Is(err, ErrForbidden) || repo.calls != 0) {
			t.Fatal(role, repo, err)
		}
		repo.calls = 0
		_, err = svc.GetBrokerSnapshot(context.Background(), tenant, user, tenant)
		if role == "" {
			if !errors.Is(err, ErrForbidden) || repo.calls != 0 {
				t.Fatal(err)
			}
		} else if err != nil || repo.calls != 1 || repo.tenant != tenant {
			t.Fatal(role, repo, err)
		}
	}
	repo := &brokerServiceRepo{}
	svc := NewImportService(repo, testImportRoles{d.TenantRoleOwner})
	if _, err := svc.UploadBrokerSnapshot(context.Background(), tenant, user, []byte("bad")); !errors.Is(err, ErrInvalid) || repo.calls != 0 {
		t.Fatal(err)
	}
	if _, err := svc.GetBrokerSnapshot(context.Background(), tenant, user, "bad"); !errors.Is(err, ErrInvalid) || repo.calls != 0 {
		t.Fatal(err)
	}
}
