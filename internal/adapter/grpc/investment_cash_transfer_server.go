package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

func protoCashTransfer(v inv.CashTransfer) *pb.InvestmentCashTransfer {
	return &pb.InvestmentCashTransfer{Timing: protoInvestmentTiming(v.Timing), Transfer: toProtoAssetTransfer(v.Transfer), LedgerId: v.LedgerID, OperationId: v.OperationID, Status: v.Status}
}
func (s *InvestmentServer) MutateInvestmentCashTransfer(ctx context.Context, req *pb.MutateInvestmentCashTransferRequest) (*pb.MutateInvestmentCashTransferResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	m, e := parseCashTransferMutation(req, t, u)
	if e != nil {
		return nil, e
	}
	saved, e := s.svc.CashTransfer(ctx, t, u, m)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.MutateInvestmentCashTransferResponse{Transfer: protoCashTransfer(saved)}, nil
}
func (s *InvestmentServer) GetInvestmentCashTransfer(ctx context.Context, req *pb.GetInvestmentCashTransferRequest) (*pb.GetInvestmentCashTransferResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.svc.ReadCashTransfer(ctx, t, u, req.GetId())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.GetInvestmentCashTransferResponse{Transfer: protoCashTransfer(v)}, nil
}
