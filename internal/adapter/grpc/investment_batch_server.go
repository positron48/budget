package grpcadapter

import (
	"context"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

func parseInvestmentBatch(p *pb.InvestmentImportBatch) (inv.ImportBatch, error) {
	if p == nil || len(p.GetEntries()) > inv.MaxBatchOperations {
		return inv.ImportBatch{}, investmentRPCError(d.ErrInvestmentInvalid)
	}
	out := inv.ImportBatch{PreparationEvidenceJSON: p.GetPreparationEvidenceJson(), ImportID: p.GetImportId(), PreviewHash: p.GetPreviewHash(), RequestKey: p.GetRequestKey(), ExpectedImportVersion: p.GetExpectedImportVersion(), ExpectedLedgerVersions: p.GetExpectedLedgerVersions()}
	for _, entry := range p.GetEntries() {
		op, err := parseInvestmentOperation(entry.GetOperation())
		if err != nil {
			return out, err
		}
		// Preserve transfer markers so the domain rejects attempts to use a
		// standalone source row in place of an atomic linked transfer.
		op.TransferID = entry.GetOperation().GetTransferId()
		op.CounterpartyLedgerID = entry.GetOperation().GetCounterpartyLedgerId()
		// Restored drafts carry the normalized timestamp plus its quality flag.
		// Preserve an inferred same-time settlement instead of silently promoting it
		// to a confirmed settlement date on a read/save round trip.
		if entry.GetOperation().GetSettlementAssumed() {
			if !op.SettlementAt.IsZero() && !op.SettlementAt.Equal(op.OccurredAt) {
				return out, investmentRPCError(d.ErrInvestmentInvalid)
			}
			op.SettlementAt = time.Time{}
		}
		out.Entries = append(out.Entries, inv.ImportBatchEntry{Record: int(entry.GetRecord()), ExpectedRowVersion: entry.GetExpectedRowVersion(), ReviewReason: entry.GetReviewReason(), Operation: op})
	}
	return out, nil
}
func protoInvestmentBatchResult(r inv.ImportBatchResult) *pb.InvestmentImportBatchResult {
	out := &pb.InvestmentImportBatchResult{Hash: r.Hash, Committed: r.Committed}
	for _, op := range r.Operations {
		out.Operations = append(out.Operations, protoInvestmentOperation(op))
	}
	for _, snapshot := range r.Snapshots {
		out.Snapshots = append(out.Snapshots, protoInvestmentSnapshot(snapshot))
	}
	return out
}
func (s *InvestmentServer) PreviewInvestmentImportBatch(ctx context.Context, req *pb.PreviewInvestmentImportBatchRequest) (*pb.PreviewInvestmentImportBatchResponse, error) {
	tenant, actor, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	batch, err := parseInvestmentBatch(req.GetBatch())
	if err != nil {
		return nil, err
	}
	result, err := s.svc.PreviewImportBatch(ctx, tenant, actor, batch)
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.PreviewInvestmentImportBatchResponse{Result: protoInvestmentBatchResult(result)}, nil
}
func (s *InvestmentServer) CommitInvestmentImportBatch(ctx context.Context, req *pb.CommitInvestmentImportBatchRequest) (*pb.CommitInvestmentImportBatchResponse, error) {
	tenant, actor, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	batch, err := parseInvestmentBatch(req.GetBatch())
	if err != nil {
		return nil, err
	}
	result, err := s.svc.CommitImportBatch(ctx, tenant, actor, batch, req.GetExpectedHash())
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.CommitInvestmentImportBatchResponse{Result: protoInvestmentBatchResult(result)}, nil
}
