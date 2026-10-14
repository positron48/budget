package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

type trackerHistoryRepo interface {
	TrackerHistory(context.Context, string, string, nav.TrackerHistoryRequest) (nav.TrackerHistory, error)
}

func (s *InvestmentNAVServer) GetTrackerHistory(ctx context.Context, req *pb.GetTrackerHistoryRequest) (*pb.GetTrackerHistoryResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	q, e := nav.NormalizeTrackerHistory(nav.TrackerHistoryRequest{LedgerIDs: req.GetLedgerIds(), Currency: req.GetCurrency(), FXPolicyKey: req.GetFxPolicyKey(), From: req.GetFromDate(), To: req.GetToDate()}, time.Now().UTC())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	repo, ok := s.repo.(trackerHistoryRepo)
	if !ok {
		return nil, status.Error(codes.Unavailable, "portfolio history unavailable")
	}
	result, e := repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetTrackerHistoryResponse{Currency: result.Currency, BasisKey: result.BasisKey, FxPolicyKey: result.FXPolicyKey, FxPolicyVersion: result.FXPolicyVersion, FxMode: result.FXMode, NextDate: result.NextDate, Generation: result.Generation, ReadAt: optionalInvestmentTimestamp(result.ReadAt), PriceConvention: md.RetrospectiveCloseNextUTCDay, ReportingTimezone: "Europe/Moscow"}
	for _, p := range result.Points {
		row := &pb.TrackerHistoryPoint{Date: p.Date, State: p.State, CoverageKey: p.CoverageKey, KnownValue: protoPerformanceAmount(p.KnownValue), PricedPositions: int32(p.PricedPositions), Positions: int32(p.Positions)}
		for _, issue := range p.Issues {
			row.Issues = append(row.Issues, &pb.InvestmentNAVIssue{ComponentId: issue.ComponentID, Code: issue.Code})
		}
		out.Points = append(out.Points, row)
	}
	return out, nil
}
