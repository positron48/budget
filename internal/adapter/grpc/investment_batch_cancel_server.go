package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func protoBatchCancellation(v inv.BatchCancellationResult) *pb.InvestmentBatchCancellationResult {
	out := &pb.InvestmentBatchCancellationResult{Hash: v.Hash, Reason: v.Reason, Cancelled: v.Cancelled, AlreadyVoided: v.AlreadyVoided}
	if !v.CancelledAt.IsZero() {
		out.CancelledAt = timestamppb.New(v.CancelledAt)
	}
	for _, op := range v.Operations {
		out.Operations = append(out.Operations, protoInvestmentOperation(op))
	}
	for _, snapshot := range v.Snapshots {
		out.Snapshots = append(out.Snapshots, protoInvestmentSnapshot(snapshot))
	}
	return out
}
func parseBatchCancellation(v *pb.InvestmentBatchCancellation) inv.BatchCancellation {
	return inv.BatchCancellation{DraftID: v.GetDraftId(), ExpectedDraftVersion: v.GetExpectedDraftVersion(), RequestKey: v.GetRequestKey(), Reason: v.GetReason()}
}
func (s *InvestmentServer) PreviewInvestmentBatchCancellation(ctx context.Context, req *pb.PreviewInvestmentBatchCancellationRequest) (*pb.PreviewInvestmentBatchCancellationResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.svc.PreviewBatchCancellation(ctx, t, u, parseBatchCancellation(req.GetCancellation()))
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.PreviewInvestmentBatchCancellationResponse{Result: protoBatchCancellation(result)}, nil
}
func (s *InvestmentServer) CommitInvestmentBatchCancellation(ctx context.Context, req *pb.CommitInvestmentBatchCancellationRequest) (*pb.CommitInvestmentBatchCancellationResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.svc.CommitBatchCancellation(ctx, t, u, parseBatchCancellation(req.GetCancellation()), req.GetExpectedHash())
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.CommitInvestmentBatchCancellationResponse{Result: protoBatchCancellation(result)}, nil
}
