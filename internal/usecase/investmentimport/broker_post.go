package investmentimport

import (
	"context"
	"github.com/google/uuid"
	"strings"
)

type BrokerTradePost struct {
	SnapshotID, BrokerOperationID, LedgerID, RequestKey, Reason string
	ExpectedLedgerVersion                                       int64
	ReconciliationConfirmed                                     bool
}
type BrokerTradePostResult struct{ OperationID, LinkID string }

func ValidateBrokerTradePost(p BrokerTradePost) error {
	for _, id := range []string{p.SnapshotID, p.LedgerID} {
		if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
			return ErrInvalid
		}
	}
	if p.ExpectedLedgerVersion < 1 || !p.ReconciliationConfirmed || len(p.BrokerOperationID) == 0 || len(p.BrokerOperationID) > 200 || len(p.RequestKey) == 0 || len(p.RequestKey) > 128 || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 2000 {
		return ErrInvalid
	}
	return nil
}
func (s *ImportService) PostBrokerTrade(ctx context.Context, tenant, user string, p BrokerTradePost) (BrokerTradePostResult, error) {
	if err := s.authorize(ctx, tenant, user, true); err != nil {
		return BrokerTradePostResult{}, err
	}
	if err := ValidateBrokerTradePost(p); err != nil {
		return BrokerTradePostResult{}, err
	}
	repo, ok := s.repo.(interface {
		PostBrokerTrade(context.Context, string, string, BrokerTradePost) (BrokerTradePostResult, error)
	})
	if !ok {
		return BrokerTradePostResult{}, ErrInvalid
	}
	return repo.PostBrokerTrade(ctx, tenant, user, p)
}
