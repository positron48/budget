package investment

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
)

const MaxBatchOperations = 5000

type ImportBatchEntry struct {
	Record             int
	ExpectedRowVersion int64
	ReviewReason       string
	Operation          d.InvestmentOperation
}
type ImportBatch struct {
	PreparationEvidenceJSON           string `json:",omitempty"` // origin evidence, retained when a reviewer edits candidates
	ImportID, PreviewHash, RequestKey string
	ExpectedImportVersion             int64
	ExpectedLedgerVersions            map[string]int64
	Entries                           []ImportBatchEntry // stable order resolves operations at identical timestamps
}
type ImportBatchResult struct {
	Hash       string
	Committed  bool
	Operations []d.InvestmentOperation
	Snapshots  []d.InvestmentSnapshot
}

func (s *Service) PreviewImportBatch(ctx context.Context, t, u string, b ImportBatch) (ImportBatchResult, error) {
	return s.importBatch(ctx, t, u, b, "", false)
}
func (s *Service) CommitImportBatch(ctx context.Context, t, u string, b ImportBatch, expectedHash string) (ImportBatchResult, error) {
	return s.importBatch(ctx, t, u, b, expectedHash, true)
}
func (s *Service) importBatch(ctx context.Context, t, u string, b ImportBatch, expectedHash string, commit bool) (ImportBatchResult, error) {
	var empty ImportBatchResult
	if err := s.authorize(ctx, t, u, true); err != nil {
		return empty, err
	}
	if commit {
		raw, err := hex.DecodeString(expectedHash)
		if err != nil || len(raw) != 32 || strings.ToLower(expectedHash) != expectedHash {
			return empty, d.ErrInvestmentInvalid
		}
	}
	b, err := NormalizeImportBatch(t, u, b)
	if err != nil {
		return empty, err
	}
	return s.repo.ImportBatch(ctx, t, u, b, expectedHash, commit)
}

// NormalizeImportBatch strips client-assigned server metadata and binds every
// entry to one reviewed immutable import row. Linked transfers use their own API.
func NormalizeImportBatch(t, u string, b ImportBatch) (ImportBatch, error) {
	canonicalID := func(value string) bool {
		parsed, err := uuid.Parse(value)
		return err == nil && parsed.String() == value
	}
	if !canonicalID(t) || !canonicalID(u) {
		return b, d.ErrInvestmentInvalid
	}
	if !canonicalID(b.ImportID) || b.ExpectedImportVersion < 1 || len(b.PreviewHash) != 64 || len(b.RequestKey) < 1 || len(b.RequestKey) > 128 || len(b.Entries) == 0 || len(b.Entries) > MaxBatchOperations || len(b.ExpectedLedgerVersions) == 0 || len(b.ExpectedLedgerVersions) > 20 {
		return b, d.ErrInvestmentInvalid
	}
	if raw, err := hex.DecodeString(b.PreviewHash); err != nil || len(raw) != 32 {
		return b, d.ErrInvestmentInvalid
	}
	if len(b.PreparationEvidenceJSON) > 2<<20 || b.PreparationEvidenceJSON != "" && !json.Valid([]byte(b.PreparationEvidenceJSON)) {
		return b, d.ErrInvestmentInvalid
	}
	b.Entries = append([]ImportBatchEntry(nil), b.Entries...)
	seen := map[int]bool{}
	used := map[string]bool{}
	for i, entry := range b.Entries {
		op := entry.Operation
		if entry.Record < 1 || entry.Record > 100000 || seen[entry.Record] || entry.ExpectedRowVersion < 1 || len(strings.TrimSpace(entry.ReviewReason)) == 0 || len(entry.ReviewReason) > 2000 || !canonicalID(op.LedgerID) || b.ExpectedLedgerVersions[op.LedgerID] < 1 {
			return b, d.ErrInvestmentInvalid
		}
		if op.BudgetTransferID != "" || op.TransferID != "" || op.CounterpartyLedgerID != "" || op.Kind == "transfer_in" || op.Kind == "transfer_out" {
			return b, invalid("use linked transfer API")
		}
		if err := ValidateOperation(&op); err != nil {
			return b, err
		}
		if operationIsFuture(op, time.Now().UTC()) {
			return b, invalid("confirmed operation cannot be future-dated")
		}
		op.ID, op.TenantID, op.ActorID = "", t, u
		op.Version, op.Sequence = 0, 0
		op.Status = ""
		op.CreatedAt = time.Time{}
		op.Source = nil
		entry.Operation = op
		b.Entries[i] = entry
		seen[entry.Record], used[op.LedgerID] = true, true
	}
	if len(used) != len(b.ExpectedLedgerVersions) {
		return b, d.ErrInvestmentInvalid
	}
	return b, nil
}
