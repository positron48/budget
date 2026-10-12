package investmentimport

import (
	"context"
	"errors"
	d "github.com/positron48/budget/internal/domain"
	"testing"
)

func TestBrokerPostRequiresExplicitReconciliation(t *testing.T) {
	p := BrokerTradePost{SnapshotID: "11111111-1111-4111-8111-111111111111", LedgerID: "22222222-2222-4222-8222-222222222222", BrokerOperationID: "trade", RequestKey: "post", ExpectedLedgerVersion: 2, Reason: "Checked all sources", ReconciliationConfirmed: true}
	if err := ValidateBrokerTradePost(p); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*BrokerTradePost){func(p *BrokerTradePost) { p.ReconciliationConfirmed = false }, func(p *BrokerTradePost) { p.ExpectedLedgerVersion = 0 }, func(p *BrokerTradePost) { p.RequestKey = "" }, func(p *BrokerTradePost) { p.Reason = " " }, func(p *BrokerTradePost) { p.LedgerID = "bad" }} {
		bad := p
		change(&bad)
		if err := ValidateBrokerTradePost(bad); err == nil {
			t.Fatal(bad)
		}
	}
}

type brokerPostRepo struct {
	ImportRepo
	calls         int
	tenant, actor string
	request       BrokerTradePost
}

func (r *brokerPostRepo) PostBrokerTrade(_ context.Context, tenant, actor string, p BrokerTradePost) (BrokerTradePostResult, error) {
	r.calls++
	r.tenant = tenant
	r.actor = actor
	r.request = p
	return BrokerTradePostResult{OperationID: "op", LinkID: "link"}, nil
}
func TestBrokerPostingRoleAndPayload(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const actor = "22222222-2222-4222-8222-222222222222"
	p := BrokerTradePost{SnapshotID: tenant, LedgerID: actor, BrokerOperationID: "trade", RequestKey: "key", ExpectedLedgerVersion: 9007199254740993, Reason: "Reviewed", ReconciliationConfirmed: true}
	for _, role := range []d.TenantRole{"", d.TenantRoleMember, d.TenantRoleAdmin, d.TenantRoleOwner} {
		repo := &brokerPostRepo{}
		svc := NewImportService(repo, testImportRoles{role})
		result, err := svc.PostBrokerTrade(context.Background(), tenant, actor, p)
		if role == d.TenantRoleAdmin || role == d.TenantRoleOwner {
			if err != nil || result.OperationID != "op" || repo.calls != 1 || repo.tenant != tenant || repo.actor != actor || repo.request != p {
				t.Fatal(result, repo, err)
			}
		} else if !errors.Is(err, ErrForbidden) || repo.calls != 0 {
			t.Fatal(repo, err)
		}
	}
	repo := &brokerPostRepo{}
	svc := NewImportService(repo, testImportRoles{d.TenantRoleOwner})
	p.ReconciliationConfirmed = false
	if _, err := svc.PostBrokerTrade(context.Background(), tenant, actor, p); !errors.Is(err, ErrInvalid) || repo.calls != 0 {
		t.Fatal(err, repo)
	}
}
