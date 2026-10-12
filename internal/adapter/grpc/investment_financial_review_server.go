package grpcadapter

import (
	"context"
	"errors"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

func (s *InvestmentImportServer) PreviewInvestmentFinancialMapping(ctx context.Context, req *pb.PreviewInvestmentFinancialMappingRequest) (*pb.PreviewInvestmentFinancialMappingResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	mapping, err := imp.DecodeFinancialMapping(req.GetMappingJson())
	if err != nil {
		return nil, investmentImportError(err)
	}
	limit := int(req.GetLimit())
	if limit == 0 {
		limit = 200
	}
	result, err := s.svc.PreviewFinancial(ctx, t, u, imp.FinancialReviewRequest{ImportID: req.GetImportId(), ExpectedImportVersion: req.GetExpectedImportVersion(), RequestKey: req.GetRequestKey(), Mapping: mapping, Offset: int(req.GetOffset()), Limit: limit})
	if err != nil {
		if errors.Is(err, d.ErrInvestmentForbidden) {
			return nil, investmentRPCError(err)
		}
		return nil, investmentImportError(err)
	}
	out := &pb.PreviewInvestmentFinancialMappingResponse{Hash: result.Hash, Ready: int32(result.Ready), Unresolved: int32(result.Unresolved), Total: int32(result.Total)}
	for _, row := range result.Rows {
		v := &pb.InvestmentFinancialCandidate{Record: int32(row.Record), SourceRowId: row.SourceRowID, ReviewReason: row.ReviewReason, Issues: row.Issues}
		if row.Operation != nil {
			v.Operation = protoInvestmentOperation(*row.Operation)
		}
		out.Rows = append(out.Rows, v)
	}
	if result.Batch != nil {
		out.Batch = protoInvestmentBatch(*result.Batch)
	}
	return out, nil
}
