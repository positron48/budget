package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
)

func (s *InvestmentNAVServer) GetInvestmentOverview(ctx context.Context, req *pb.GetInvestmentOverviewRequest) (*pb.GetInvestmentOverviewResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.repo.OverviewForSeries(ctx, tenant, actor, req.GetSeriesKey(), req.GetFromDate(), req.GetToDate())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	performance := protoInvestmentPerformance(v.Performance)
	performance.TwrPoints = nil
	out := &pb.GetInvestmentOverviewResponse{SeriesKey: v.Performance.SeriesKey, Performance: performance, Nav: protoNAVPoint(v.NAV), DailyReason: v.Daily.DailyReason, ReadAt: optionalInvestmentTimestamp(v.ReadAt), Scope: "full_registered_accounts"}
	if v.Daily.DailyReason == "" {
		out.DailyProfit = protoPerformanceAmount(v.Daily.Daily)
	}
	if (v.NAV.State == "ready" || v.NAV.State == "incomplete") && v.Composition != nil {
		out.SecuritiesKnown = protoPerformanceAmount(v.Composition.SecuritiesKnown)
		out.SecuritiesComplete = v.Composition.SecuritiesComplete
		out.CashKnown = protoPerformanceAmount(v.Composition.CashKnown)
		out.CashComplete = v.Composition.CashComplete
		for _, issue := range v.Composition.Issues {
			out.BreakdownIssues = append(out.BreakdownIssues, &pb.InvestmentNAVIssue{ComponentId: issue.ComponentID, Code: issue.Code})
		}
	}
	for _, quote := range v.FX {
		out.Fx = append(out.Fx, &pb.InvestmentOverviewFX{FromCurrency: quote.From, ToCurrency: quote.To, PolicyKey: quote.Policy, PolicyVersion: quote.PolicyVersion, Source: quote.Source, Mode: quote.Mode, State: quote.State, Rate: protoPerformanceAmount(quote.Rate), Change: protoPerformanceAmount(quote.Change), AsOf: optionalInvestmentTimestamp(quote.At), AvailableAt: optionalInvestmentTimestamp(quote.AvailableAt), ValidUntil: optionalInvestmentTimestamp(quote.ValidUntil), Revision: quote.Revision})
	}
	return out, nil
}
