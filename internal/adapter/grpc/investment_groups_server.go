package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

func protoInstrumentGroups(p nav.InstrumentGroups) *pb.InvestmentGroups {
	out := &pb.InvestmentGroups{Version: p.Version}
	for _, g := range p.Groups {
		out.Groups = append(out.Groups, &pb.InvestmentGroup{Id: g.ID, Name: g.Name})
	}
	for _, m := range p.Memberships {
		out.Memberships = append(out.Memberships, &pb.InvestmentGroupMembership{LedgerId: m.LedgerID, InstrumentKey: m.InstrumentKey, GroupId: m.GroupID})
	}
	return out
}
func (s *InvestmentNAVServer) GetInvestmentGroups(ctx context.Context, req *pb.GetInvestmentGroupsRequest) (*pb.GetInvestmentGroupsResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	p, positions, e := s.repo.GetInstrumentGroups(ctx, tenant, actor)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentGroupsResponse{Classification: protoInstrumentGroups(p)}
	for _, v := range positions {
		out.Positions = append(out.Positions, &pb.InvestmentGroupPosition{LedgerId: v.LedgerID, LedgerName: v.LedgerName, InstrumentKey: v.InstrumentKey})
	}
	return out, nil
}
func (s *InvestmentNAVServer) SaveInvestmentGroups(ctx context.Context, req *pb.SaveInvestmentGroupsRequest) (*pb.SaveInvestmentGroupsResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	c := req.GetClassification()
	p := nav.SaveInstrumentGroups{InstrumentGroups: nav.InstrumentGroups{Version: c.GetVersion()}, RequestKey: req.GetRequestKey(), Reason: req.GetReason()}
	for _, g := range c.GetGroups() {
		p.Groups = append(p.Groups, nav.InstrumentGroup{ID: g.GetId(), Name: g.GetName()})
	}
	for _, m := range c.GetMemberships() {
		p.Memberships = append(p.Memberships, nav.GroupMembership{LedgerID: m.GetLedgerId(), InstrumentKey: m.GetInstrumentKey(), GroupID: m.GetGroupId()})
	}
	out, e := s.repo.SaveInstrumentGroups(ctx, tenant, actor, p)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.SaveInvestmentGroupsResponse{Classification: protoInstrumentGroups(out)}, nil
}

func (s *InvestmentNAVServer) GetInvestmentCompositionHistory(ctx context.Context, req *pb.GetInvestmentCompositionHistoryRequest) (*pb.GetInvestmentCompositionHistoryResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	history, e := s.repo.InvestmentCompositionHistory(ctx, tenant, actor, req.GetSeriesKey(), req.GetFromDate(), req.GetToDate(), req.GetGroupIds(), req.GetBreakdownBy())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentCompositionHistoryResponse{BreakdownBy: history.BreakdownBy, ClassificationVersion: history.Classification.Version, Currency: history.Currency, FxPolicyKey: history.FXPolicyKey, ReportingTimezone: "Europe/Moscow"}
	for _, g := range history.Classification.Groups {
		out.Groups = append(out.Groups, &pb.InvestmentGroup{Id: g.ID, Name: g.Name})
	}
	for i, day := range history.Days {
		point := &pb.InvestmentCompositionPoint{Date: day.Date, State: day.State}
		if (day.State == "ready" || day.State == "incomplete") && day.Composition != nil {
			c := day.Composition
			point.SecuritiesKnown = protoPerformanceAmount(c.SecuritiesKnown)
			point.SecuritiesComplete = c.SecuritiesComplete
			point.CashKnown = protoPerformanceAmount(c.CashKnown)
			point.CashComplete = c.CashComplete
			for _, issue := range c.Issues {
				point.Issues = append(point.Issues, &pb.InvestmentNAVIssue{ComponentId: issue.ComponentID, Code: issue.Code})
			}
			if i == len(history.Days)-1 {
				out.Complete = c.Complete
				for _, segment := range c.Segments {
					out.Segments = append(out.Segments, &pb.InvestmentCompositionSegment{CategoryKey: segment.CategoryKey, GroupId: segment.GroupID, Kind: segment.Kind, KnownValue: protoPerformanceAmount(segment.KnownValue), Complete: segment.Complete, Weight: protoPerformanceAmount(segment.Weight)})
				}
			}
		}
		out.Points = append(out.Points, point)
	}
	return out, nil
}
