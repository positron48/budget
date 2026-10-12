package investmentimport

import (
	"context"
	"errors"
	d "github.com/positron48/budget/internal/domain"
	"testing"
)

type brokerLinkRepo struct {
	ImportRepo
	calls         int
	tenant, actor string
	link          BrokerTradeLink
}

func (r *brokerLinkRepo) LinkBrokerTrade(_ context.Context, tenant, actor string, link BrokerTradeLink) (string, error) {
	r.calls++
	r.tenant = tenant
	r.actor = actor
	r.link = link
	return "link", nil
}
func TestLinkBrokerTradeAuthorizationAndVersion(t *testing.T) {
	const tenant = "00000000-0000-0000-0000-000000000001"
	const actor = "00000000-0000-0000-0000-000000000002"
	valid := BrokerTradeLink{SnapshotID: tenant, LedgerID: tenant, OperationID: actor, BrokerOperationID: "trade", ExpectedOperationVersion: 7, Reason: "Reviewed exact match"}
	for _, role := range []d.TenantRole{"", d.TenantRoleMember, d.TenantRoleAdmin, d.TenantRoleOwner} {
		repo := &brokerLinkRepo{}
		svc := NewImportService(repo, testImportRoles{role})
		id, err := svc.LinkBrokerTrade(context.Background(), tenant, actor, valid)
		allowed := role == d.TenantRoleAdmin || role == d.TenantRoleOwner
		if allowed {
			if err != nil || id != "link" || repo.calls != 1 || repo.tenant != tenant || repo.actor != actor || repo.link != valid {
				t.Fatal(id, err, repo)
			}
		} else if !errors.Is(err, ErrForbidden) || repo.calls != 0 {
			t.Fatal(err, repo)
		}
	}
	for _, modify := range []func(*BrokerTradeLink){func(l *BrokerTradeLink) { l.ExpectedOperationVersion = 0 }, func(l *BrokerTradeLink) { l.Reason = " " }, func(l *BrokerTradeLink) { l.SnapshotID = "bad" }, func(l *BrokerTradeLink) { l.BrokerOperationID = "" }} {
		repo := &brokerLinkRepo{}
		svc := NewImportService(repo, testImportRoles{d.TenantRoleOwner})
		invalid := valid
		modify(&invalid)
		if _, err := svc.LinkBrokerTrade(context.Background(), tenant, actor, invalid); !errors.Is(err, ErrInvalid) || repo.calls != 0 {
			t.Fatal(err, repo)
		}
	}
}
