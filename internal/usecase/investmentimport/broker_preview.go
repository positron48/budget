package investmentimport

import (
	"context"
	"fmt"
	d "github.com/positron48/budget/internal/domain"
	"time"
)

// PreviewBrokerTrade prepares facts and checks a hypothetical journal addition.
// It does not reserve sources or prove absence of all Snowball equivalents.
// Posting rechecks versions, matches and source claims atomically.
func (s *ImportService) PreviewBrokerTrade(ctx context.Context, tenant, user, snapshotID, operationID, ledgerID string) (BrokerTradeCandidate, error) {
	raw, err := s.GetBrokerSnapshot(ctx, tenant, user, snapshotID)
	if err != nil {
		return BrokerTradeCandidate{}, err
	}
	snapshot, err := PrepareBrokerSnapshot(raw)
	if err != nil {
		return BrokerTradeCandidate{}, fmt.Errorf("%w: invalid stored snapshot", ErrInvalid)
	}
	candidate, err := PrepareBrokerTrade(snapshot, operationID, ledgerID)
	if err != nil {
		return BrokerTradeCandidate{}, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	repo, ok := s.repo.(interface {
		BrokerSnapshotRows(context.Context, string, string, int, int) ([]BrokerStoredRow, error)
	})
	if !ok {
		return BrokerTradeCandidate{}, fmt.Errorf("broker source storage unavailable")
	}
	claimed := false
	remaining := make(map[string]string, len(candidate.SourceIdentities))
	for i, identity := range candidate.SourceIdentities {
		remaining[identity] = candidate.SourceHashes[i]
	}
	for offset := 0; offset < len(snapshot.Rows); offset += 200 {
		rows, err := repo.BrokerSnapshotRows(ctx, tenant, snapshotID, 200, offset)
		if err != nil {
			return BrokerTradeCandidate{}, err
		}
		for _, row := range rows {
			identity := "tinvest:" + row.AccountID + ":" + row.OperationID
			expected, needed := remaining[identity]
			if !needed {
				continue
			}
			if row.HasOtherVersions || row.SHA256 != expected {
				return BrokerTradeCandidate{}, fmt.Errorf("%w: broker source has conflicting versions", ErrInvalid)
			}
			claimed = claimed || row.LinkID != ""
			delete(remaining, identity)
		}
	}
	if len(remaining) != 0 {
		return BrokerTradeCandidate{}, fmt.Errorf("%w: broker source observations missing", ErrInvalid)
	}
	journalRepo, ok := s.repo.(interface {
		BrokerTradeJournalCandidates(context.Context, string, string, time.Time) ([]d.InvestmentOperation, error)
	})
	if !ok {
		return BrokerTradeCandidate{}, fmt.Errorf("broker journal reconciliation unavailable")
	}
	var operations []d.InvestmentOperation
	if distinct, ok := s.repo.(interface {
		BrokerTradeDistinctCandidates(context.Context, string, string, time.Time, []string) ([]d.InvestmentOperation, error)
	}); ok {
		operations, err = distinct.BrokerTradeDistinctCandidates(ctx, tenant, ledgerID, candidate.Operation.OccurredAt, candidate.SourceIdentities)
	} else {
		operations, err = journalRepo.BrokerTradeJournalCandidates(ctx, tenant, ledgerID, candidate.Operation.OccurredAt)
	}
	if err != nil {
		return BrokerTradeCandidate{}, err
	}
	candidate.JournalMatches = PossibleBrokerTradeMatches(candidate.Operation, operations)
	replayRepo, ok := s.repo.(interface {
		BrokerTradeReplay(context.Context, string, d.InvestmentOperation) (BrokerReplayCheck, error)
	})
	if !ok {
		return BrokerTradeCandidate{}, fmt.Errorf("broker replay unavailable")
	}
	candidate.Replay, err = replayRepo.BrokerTradeReplay(ctx, tenant, candidate.Operation)
	if err != nil {
		return BrokerTradeCandidate{}, err
	}
	candidate.PostingAllowed = candidate.Replay.Valid && len(candidate.JournalMatches) == 0 && !claimed
	return candidate, nil
}
