package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *InvestmentServer) QueryInvestmentOperations(ctx context.Context, req *pb.QueryInvestmentOperationsRequest) (*pb.QueryInvestmentOperationsResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.svc.QueryOperations(ctx, t, u, inv.OperationsQuery{LedgerIDs: req.GetLedgerIds(), FromDate: req.GetFromDate(), ToDate: req.GetToDate(), InstrumentKey: req.GetInstrumentKey(), Kind: req.GetKind(), Status: req.GetStatus(), Origin: req.GetOrigin(), ImportID: req.GetImportId(), Cursor: req.GetCursor(), Limit: int(req.GetLimit())})
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.QueryInvestmentOperationsResponse{TotalCount: v.TotalCount, NextCursor: v.NextCursor}
	for _, l := range v.Ledgers {
		out.Ledgers = append(out.Ledgers, protoLedger(l))
	}
	for _, r := range v.Entries {
		row := &pb.InvestmentOperationRow{Operation: protoInvestmentOperation(r.Operation), LedgerName: r.LedgerName, Origins: r.Origins, BrokerEvidenceVersion: r.BrokerEvidenceVersion, UnitCleanPrice: protoPayoutAmount(r.UnitCleanPrice)}
		for _, c := range r.Cash {
			row.Cash = append(row.Cash, &pb.InvestmentOperationCash{Currency: c.Currency, Amount: protoPayoutAmount(c.Amount)})
		}
		out.Entries = append(out.Entries, row)
	}
	return out, nil
}
func (s *InvestmentServer) GetInvestmentOperationHistory(ctx context.Context, req *pb.GetInvestmentOperationHistoryRequest) (*pb.GetInvestmentOperationHistoryResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.svc.OperationHistory(ctx, t, u, inv.OperationHistoryQuery{LedgerID: req.GetLedgerId(), OperationID: req.GetOperationId(), AfterVersion: req.GetAfterVersion(), ExpectedVersion: req.GetExpectedVersion(), Limit: int(req.GetLimit())})
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentOperationHistoryResponse{Ledger: protoLedger(v.Ledger), Current: protoInvestmentOperation(v.Current), NextAfterVersion: v.NextAfterVersion}
	for _, r := range v.Revisions {
		rev := &pb.InvestmentOperationRevision{Version: r.Version, Action: r.Action, ActorId: r.ActorID, Reason: r.Reason, RecordedAt: timestamppb.New(r.RecordedAt), After: protoInvestmentOperation(r.After)}
		if r.Before != nil {
			rev.Before = protoInvestmentOperation(*r.Before)
		}
		out.Revisions = append(out.Revisions, rev)
	}
	return out, nil
}
