package investmentimport

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"time"
)

type BrokerSourceRepo interface {
	SaveBrokerSnapshot(context.Context, string, string, []byte) (string, error)
	BrokerSnapshotBytes(context.Context, string, string) ([]byte, error)
}

func (s *ImportService) UploadBrokerSnapshot(ctx context.Context, tenant, user string, raw []byte) (string, error) {
	if err := s.authorize(ctx, tenant, user, true); err != nil {
		return "", err
	}
	if len(raw) > 20<<20 {
		return "", ErrInvalid
	} // leaves room for the gRPC envelope
	if _, err := PrepareBrokerSnapshot(raw); err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	repo, ok := s.repo.(BrokerSourceRepo)
	if !ok {
		return "", fmt.Errorf("broker source storage unavailable")
	}
	return repo.SaveBrokerSnapshot(ctx, tenant, user, raw)
}
func (s *ImportService) GetBrokerSnapshot(ctx context.Context, tenant, user, id string) ([]byte, error) {
	if err := s.authorize(ctx, tenant, user, false); err != nil {
		return nil, err
	}
	if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
		return nil, ErrInvalid
	}
	repo, ok := s.repo.(BrokerSourceRepo)
	if !ok {
		return nil, fmt.Errorf("broker source storage unavailable")
	}
	return repo.BrokerSnapshotBytes(ctx, tenant, id)
}

// BrokerSnapshotInfo intentionally omits raw broker operation data.
type BrokerSnapshotInfo struct {
	ID, AccountID, SHA256 string
	Rows                  int
	CreatedAt             time.Time
}

func (s *ImportService) ListBrokerSnapshots(ctx context.Context, tenant, user string, limit, offset int) ([]BrokerSnapshotInfo, error) {
	if err := s.authorize(ctx, tenant, user, false); err != nil {
		return nil, err
	}
	if err := page(limit, offset); err != nil {
		return nil, err
	}
	repo, ok := s.repo.(interface {
		ListBrokerSnapshots(context.Context, string, int, int) ([]BrokerSnapshotInfo, error)
	})
	if !ok {
		return nil, fmt.Errorf("broker source storage unavailable")
	}
	return repo.ListBrokerSnapshots(ctx, tenant, limit, offset)
}

type BrokerStoredRow struct {
	LinkID, LinkedLedgerID, LinkedOperationID, LinkedStatus string
	LinkedVersion, CurrentVersion                           int64
	ClaimedDifferentContent                                 bool
	Ordinal                                                 int
	ObservationID, AccountID, OperationID, SHA256           string
	Raw                                                     []byte
	HasOtherVersions                                        bool
}

func (s *ImportService) BrokerSnapshotRows(ctx context.Context, tenant, user, id string, limit, offset int) ([]BrokerStoredRow, error) {
	if err := s.authorize(ctx, tenant, user, false); err != nil {
		return nil, err
	}
	if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
		return nil, ErrInvalid
	}
	if err := page(limit, offset); err != nil {
		return nil, err
	}
	repo, ok := s.repo.(interface {
		BrokerSnapshotRows(context.Context, string, string, int, int) ([]BrokerStoredRow, error)
	})
	if !ok {
		return nil, fmt.Errorf("broker source storage unavailable")
	}
	return repo.BrokerSnapshotRows(ctx, tenant, id, limit, offset)
}
