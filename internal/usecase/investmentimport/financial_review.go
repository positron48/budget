package investmentimport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"io"
)

const MaxMappingBytes = 2 << 20

type FinancialReviewRequest struct {
	ImportID, RequestKey  string
	ExpectedImportVersion int64
	Mapping               FinancialMapping
	Offset, Limit         int
}
type FinancialReview struct {
	Hash                     string
	Ready, Unresolved, Total int
	Rows                     []FinancialCandidate
	Batch                    *inv.ImportBatch
}

func DecodeFinancialMapping(raw []byte) (FinancialMapping, error) {
	var out FinancialMapping
	if len(raw) == 0 || len(raw) > MaxMappingBytes {
		return out, ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, ErrInvalid
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return out, ErrInvalid
	}
	return out, nil
}
func (s *ImportService) PreviewFinancial(ctx context.Context, t, u string, request FinancialReviewRequest) (FinancialReview, error) {
	if err := s.authorize(ctx, t, u, true); err != nil {
		return FinancialReview{}, err
	}
	id, err := uuid.Parse(request.ImportID)
	if err != nil || id.String() != request.ImportID || request.ExpectedImportVersion < 1 || len(request.RequestKey) < 1 || len(request.RequestKey) > 128 || request.Limit < 1 || request.Limit > inv.MaxBatchOperations || request.Offset < 0 || request.Offset >= MaxSourceRows {
		return FinancialReview{}, ErrInvalid
	}
	raw, err := json.Marshal(request.Mapping)
	if err != nil || len(raw) > MaxMappingBytes {
		return FinancialReview{}, fmt.Errorf("%w: financial mapping too large", ErrInvalid)
	}
	return s.repo.PreviewFinancialImport(ctx, t, u, request)
}
