package investment

import (
	"context"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
)

type BatchDraft struct {
	Cancellation *BatchCancellationResult
	ID           string
	Version      int64
	Batch        ImportBatch
	Preview      *ImportBatchResult
	UpdatedAt    time.Time
}
type BatchDraftSummary struct {
	Cancelled    bool
	ID, ImportID string
	Version      int64
	Rows         int
	Committed    bool
	UpdatedAt    time.Time
}
type BatchDraftWrite struct {
	ID              string
	ExpectedVersion int64
	Batch           ImportBatch
}

func canonicalDraftID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}
func (s *Service) SaveBatchDraft(ctx context.Context, t, u string, w BatchDraftWrite) (BatchDraft, error) {
	if err := s.authorize(ctx, t, u, true); err != nil {
		return BatchDraft{}, err
	}
	if !canonicalDraftID(w.ID) || w.ExpectedVersion < 0 {
		return BatchDraft{}, d.ErrInvestmentInvalid
	}
	b, err := NormalizeImportBatch(t, u, w.Batch)
	if err != nil {
		return BatchDraft{}, err
	}
	w.Batch = b
	return s.repo.SaveBatchDraft(ctx, t, u, w)
}
func (s *Service) GetBatchDraft(ctx context.Context, t, u, id string) (BatchDraft, error) {
	if err := s.authorize(ctx, t, u, true); err != nil {
		return BatchDraft{}, err
	}
	if !canonicalDraftID(id) {
		return BatchDraft{}, d.ErrInvestmentInvalid
	}
	return s.repo.GetBatchDraft(ctx, t, u, id)
}
func (s *Service) ListBatchDrafts(ctx context.Context, t, u, after string) ([]BatchDraftSummary, error) {
	if err := s.authorize(ctx, t, u, true); err != nil {
		return nil, err
	}
	if after != "" && !canonicalDraftID(after) {
		return nil, d.ErrInvestmentInvalid
	}
	return s.repo.ListBatchDrafts(ctx, t, u, after)
}
func (s *Service) PrepareBatchDraft(ctx context.Context, t, u, id string, version int64) (BatchDraft, error) {
	return s.runBatchDraft(ctx, t, u, id, version, "", false)
}
func (s *Service) CommitBatchDraft(ctx context.Context, t, u, id string, version int64, hash string) (BatchDraft, error) {
	if len(hash) != 64 {
		return BatchDraft{}, d.ErrInvestmentInvalid
	}
	return s.runBatchDraft(ctx, t, u, id, version, hash, true)
}
func (s *Service) runBatchDraft(ctx context.Context, t, u, id string, version int64, hash string, commit bool) (BatchDraft, error) {
	if err := s.authorize(ctx, t, u, true); err != nil {
		return BatchDraft{}, err
	}
	if !canonicalDraftID(id) || version < 1 {
		return BatchDraft{}, d.ErrInvestmentInvalid
	}
	return s.repo.RunBatchDraft(ctx, t, u, id, version, hash, commit)
}
func (s *Service) DeleteBatchDraft(ctx context.Context, t, u, id string, version int64) error {
	if err := s.authorize(ctx, t, u, true); err != nil {
		return err
	}
	if !canonicalDraftID(id) || version < 1 {
		return d.ErrInvestmentInvalid
	}
	return s.repo.DeleteBatchDraft(ctx, t, u, id, version)
}
