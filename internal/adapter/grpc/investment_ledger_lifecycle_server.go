package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *InvestmentServer) UpdateInvestmentLedger(ctx context.Context, req *pb.UpdateInvestmentLedgerRequest) (*pb.UpdateInvestmentLedgerResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	l, e := s.svc.UpdateLedger(ctx, t, u, inv.LedgerUpdate{ID: req.GetId(), Action: req.GetAction(), Name: req.GetName(), Broker: req.GetBroker(), AccountType: req.GetAccountType(), Comment: req.GetComment(), DefaultCurrency: req.GetDefaultCurrency(), ExpectedMetadataVersion: req.GetExpectedMetadataVersion(), ExpectedLedgerVersion: req.GetExpectedLedgerVersion(), RequestKey: req.GetRequestKey(), Reason: req.GetReason()})
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.UpdateInvestmentLedgerResponse{Ledger: protoLedger(l)}, nil
}
func (s *InvestmentServer) GetInvestmentLedgerHistory(ctx context.Context, req *pb.GetInvestmentLedgerHistoryRequest) (*pb.GetInvestmentLedgerHistoryResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	h, e := s.svc.LedgerHistory(ctx, t, u, inv.LedgerHistoryQuery{ID: req.GetId(), AfterVersion: req.GetAfterVersion(), ExpectedMetadataVersion: req.GetExpectedMetadataVersion(), Limit: int(req.GetLimit())})
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentLedgerHistoryResponse{Ledger: protoLedger(h.Ledger), NextAfterVersion: h.NextAfterVersion}
	for _, v := range h.Revisions {
		r := &pb.InvestmentLedgerRevision{Version: v.Version, Action: v.Action, ActorId: v.ActorID, Reason: v.Reason, RecordedAt: timestamppb.New(v.RecordedAt)}
		if v.Before != nil {
			r.Before = protoLedger(*v.Before)
		}
		if v.After != nil {
			r.After = protoLedger(*v.After)
		}
		out.Revisions = append(out.Revisions, r)
	}
	return out, nil
}
