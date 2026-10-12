package investmentimport

import (
	"context"
	"github.com/google/uuid"
	"strings"
)

type BrokerTradeLink struct {
	SnapshotID, BrokerOperationID, LedgerID, OperationID, Reason string
	ExpectedOperationVersion                                     int64
}

func ValidateBrokerTradeLink(link BrokerTradeLink) error {
	for _, id := range []string{link.SnapshotID, link.LedgerID, link.OperationID} {
		if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
			return ErrInvalid
		}
	}
	if link.ExpectedOperationVersion < 1 || len(link.BrokerOperationID) == 0 || len(link.BrokerOperationID) > 200 || strings.TrimSpace(link.Reason) == "" || len(link.Reason) > 2000 {
		return ErrInvalid
	}
	return nil
}
func (s *ImportService) LinkBrokerTrade(ctx context.Context, tenant, user string, link BrokerTradeLink) (string, error) {
	if err := s.authorize(ctx, tenant, user, true); err != nil {
		return "", err
	}
	if err := ValidateBrokerTradeLink(link); err != nil {
		return "", err
	}
	repo, ok := s.repo.(interface {
		LinkBrokerTrade(context.Context, string, string, BrokerTradeLink) (string, error)
	})
	if !ok {
		return "", ErrInvalid
	}
	return repo.LinkBrokerTrade(ctx, tenant, user, link)
}
