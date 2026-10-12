package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *InvestmentServer) PreviewInvestmentOperation(ctx context.Context, req *pb.PreviewInvestmentOperationRequest) (*pb.PreviewInvestmentOperationResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	var o d.InvestmentOperation
	if req.GetAction() == "void" {
		o = d.InvestmentOperation{ID: req.GetOperation().GetId(), LedgerID: req.GetOperation().GetLedgerId()}
	} else {
		o, e = parseInvestmentOperation(req.GetOperation())
		if e != nil {
			return nil, e
		}
	}
	v, e := s.svc.PreviewMutation(ctx, t, u, inv.Mutation{Action: req.GetAction(), Operation: o, Source: parseInvestmentSource(req.GetSource()), ExpectedLedgerVersion: req.GetExpectedLedgerVersion(), ExpectedOperationVersion: req.GetExpectedOperationVersion(), RequestKey: req.GetRequestKey(), Reason: req.GetReason()})
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return protoMutationPreview(v), nil
}
func protoMutationPreview(v inv.MutationPreview) *pb.PreviewInvestmentOperationResponse {
	out := &pb.PreviewInvestmentOperationResponse{Valid: v.Valid, PreviewHash: v.Hash, CheckedAt: timestamppb.New(v.CheckedAt), DirtyFrom: optionalInvestmentTimestamp(v.DirtyFrom), AffectedLedgerIds: v.AffectedLedgerIDs, Proposed: protoInvestmentOperation(v.Proposed)}
	for _, i := range v.Issues {
		item := &pb.InvestmentPreviewIssue{Code: i.Code, OperationId: i.OperationID, InstrumentKey: i.InstrumentKey, At: optionalInvestmentTimestamp(i.At)}
		if i.Cash != nil {
			c := i.Cash
			item.Cash = &pb.InvestmentCashBalance{Currency: c.Currency, Settled: c.Settled, Receivable: c.Receivable, Payable: c.Payable, Economic: c.Economic}
		}
		out.Issues = append(out.Issues, item)
	}
	for _, c := range v.Comparisons {
		changes := protoInvestmentSnapshot(d.InvestmentSnapshot{Cash: c.CashChange, Positions: c.PositionChange})
		out.Comparisons = append(out.Comparisons, &pb.InvestmentPreviewComparison{Stage: c.Stage, AsOf: timestamppb.New(c.At), Before: protoInvestmentSnapshot(c.Before), After: protoInvestmentSnapshot(c.After), CashChange: changes.Cash, PositionChange: changes.Positions})
	}
	return out
}
