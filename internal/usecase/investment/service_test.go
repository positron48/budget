package investment

import (
	"context"
	"errors"
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

type roleFixture struct{ role d.TenantRole }

func (r roleFixture) GetUserRole(context.Context, string, string) (d.TenantRole, error) {
	return r.role, nil
}
func TestJournalRoleBoundaries(t *testing.T) {
	tenant := "00000000-0000-0000-0000-000000000001"
	user := "00000000-0000-0000-0000-000000000002"
	for _, role := range []d.TenantRole{"", d.TenantRoleMember} {
		svc := NewService(nil, roleFixture{role})
		if _, err := svc.CreateLedger(context.Background(), tenant, user, "test", time.Now(), "key"); !errors.Is(err, d.ErrInvestmentForbidden) {
			t.Fatal(role, err)
		}
	}
	svc := NewService(nil, roleFixture{d.TenantRoleMember})
	if _, err := svc.Mutate(context.Background(), tenant, user, Mutation{}); !errors.Is(err, d.ErrInvestmentInvalid) {
		t.Fatal("member blocked before input validation", err)
	}
	svc = NewService(nil, roleFixture{})
	if _, err := svc.Mutate(context.Background(), tenant, user, Mutation{}); !errors.Is(err, d.ErrInvestmentForbidden) {
		t.Fatal("nonmember allowed", err)
	}
}

func TestSourcePostingRequiresAdmin(t *testing.T) {
	svc := NewService(nil, roleFixture{d.TenantRoleMember})
	_, err := svc.Mutate(context.Background(), "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002", Mutation{Source: &d.InvestmentSource{}})
	if !errors.Is(err, d.ErrInvestmentForbidden) {
		t.Fatal(err)
	}
}
