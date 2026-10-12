package investment

import (
	"context"
	d "github.com/positron48/budget/internal/domain"
	"strings"
	"time"
)

type BatchCancellation struct {
	DraftID, RequestKey, Reason string
	ExpectedDraftVersion        int64
}
type BatchCancellationResult struct {
	Hash, Reason  string
	Cancelled     bool
	CancelledAt   time.Time
	Operations    []d.InvestmentOperation
	AlreadyVoided []string
	Snapshots     []d.InvestmentSnapshot
}

func ValidateBatchCancellation(c BatchCancellation) error {
	if !canonicalDraftID(c.DraftID) || c.ExpectedDraftVersion < 1 || len(c.RequestKey) < 1 || len(c.RequestKey) > 128 || len(strings.TrimSpace(c.Reason)) == 0 || len(c.Reason) > 2000 {
		return d.ErrInvestmentInvalid
	}
	return nil
}
func (s *Service) PreviewBatchCancellation(ctx context.Context, t, u string, c BatchCancellation) (BatchCancellationResult, error) {
	return s.cancelBatch(ctx, t, u, c, "", false)
}
func (s *Service) CommitBatchCancellation(ctx context.Context, t, u string, c BatchCancellation, hash string) (BatchCancellationResult, error) {
	if len(hash) != 64 {
		return BatchCancellationResult{}, d.ErrInvestmentInvalid
	}
	return s.cancelBatch(ctx, t, u, c, hash, true)
}
func (s *Service) cancelBatch(ctx context.Context, t, u string, c BatchCancellation, hash string, commit bool) (BatchCancellationResult, error) {
	if err := s.authorize(ctx, t, u, true); err != nil {
		return BatchCancellationResult{}, err
	}
	if err := ValidateBatchCancellation(c); err != nil {
		return BatchCancellationResult{}, err
	}
	return s.repo.CancelBatch(ctx, t, u, c, hash, commit)
}
