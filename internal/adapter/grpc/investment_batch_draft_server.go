package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func protoInvestmentDraft(v inv.BatchDraft) *pb.InvestmentImportDraft {
	batch := protoInvestmentBatch(v.Batch)
	out := &pb.InvestmentImportDraft{Id: v.ID, Version: v.Version, Batch: batch, UpdatedAt: timestamppb.New(v.UpdatedAt)}
	if v.Preview != nil {
		out.Preview = protoInvestmentBatchResult(*v.Preview)
	}
	if v.Cancellation != nil {
		out.Cancellation = protoBatchCancellation(*v.Cancellation)
	}
	return out
}
func (s *InvestmentServer) SaveInvestmentImportDraft(ctx context.Context, req *pb.SaveInvestmentImportDraftRequest) (*pb.SaveInvestmentImportDraftResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	batch, err := parseInvestmentBatch(req.GetBatch())
	if err != nil {
		return nil, err
	}
	draft, err := s.svc.SaveBatchDraft(ctx, t, u, inv.BatchDraftWrite{ID: req.GetId(), ExpectedVersion: req.GetExpectedVersion(), Batch: batch})
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.SaveInvestmentImportDraftResponse{Draft: protoInvestmentDraft(draft)}, nil
}
func (s *InvestmentServer) GetInvestmentImportDraft(ctx context.Context, req *pb.GetInvestmentImportDraftRequest) (*pb.GetInvestmentImportDraftResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	draft, err := s.svc.GetBatchDraft(ctx, t, u, req.GetId())
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.GetInvestmentImportDraftResponse{Draft: protoInvestmentDraft(draft)}, nil
}
func (s *InvestmentServer) ListInvestmentImportDrafts(ctx context.Context, req *pb.ListInvestmentImportDraftsRequest) (*pb.ListInvestmentImportDraftsResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.svc.ListBatchDrafts(ctx, t, u, req.GetAfterId())
	if err != nil {
		return nil, investmentRPCError(err)
	}
	out := &pb.ListInvestmentImportDraftsResponse{}
	for _, v := range list {
		out.Drafts = append(out.Drafts, &pb.InvestmentImportDraftSummary{Id: v.ID, ImportId: v.ImportID, Version: v.Version, RowCount: int32(v.Rows), Committed: v.Committed, Cancelled: v.Cancelled, UpdatedAt: timestamppb.New(v.UpdatedAt)})
	}
	return out, nil
}
func (s *InvestmentServer) PreviewInvestmentImportDraft(ctx context.Context, req *pb.PreviewInvestmentImportDraftRequest) (*pb.PreviewInvestmentImportDraftResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	draft, err := s.svc.PrepareBatchDraft(ctx, t, u, req.GetId(), req.GetExpectedVersion())
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.PreviewInvestmentImportDraftResponse{Draft: protoInvestmentDraft(draft)}, nil
}
func (s *InvestmentServer) CommitInvestmentImportDraft(ctx context.Context, req *pb.CommitInvestmentImportDraftRequest) (*pb.CommitInvestmentImportDraftResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	draft, err := s.svc.CommitBatchDraft(ctx, t, u, req.GetId(), req.GetExpectedVersion(), req.GetExpectedHash())
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.CommitInvestmentImportDraftResponse{Draft: protoInvestmentDraft(draft)}, nil
}
func (s *InvestmentServer) DeleteInvestmentImportDraft(ctx context.Context, req *pb.DeleteInvestmentImportDraftRequest) (*pb.DeleteInvestmentImportDraftResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.svc.DeleteBatchDraft(ctx, t, u, req.GetId(), req.GetExpectedVersion()); err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.DeleteInvestmentImportDraftResponse{}, nil
}

func protoInvestmentBatch(b inv.ImportBatch) *pb.InvestmentImportBatch {
	batch := &pb.InvestmentImportBatch{PreparationEvidenceJson: b.PreparationEvidenceJSON, ImportId: b.ImportID, PreviewHash: b.PreviewHash, RequestKey: b.RequestKey, ExpectedImportVersion: b.ExpectedImportVersion, ExpectedLedgerVersions: b.ExpectedLedgerVersions}
	for _, entry := range b.Entries {
		batch.Entries = append(batch.Entries, &pb.InvestmentImportBatchEntry{Record: int32(entry.Record), ExpectedRowVersion: entry.ExpectedRowVersion, ReviewReason: entry.ReviewReason, Operation: protoInvestmentOperation(entry.Operation)})
	}
	return batch
}
