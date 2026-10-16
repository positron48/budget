package grpcadapter

import (
	"context"
	"math/big"
	"sort"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type trackerHistoryRepo interface {
	TrackerHistory(context.Context, string, string, nav.TrackerHistoryRequest) (nav.TrackerHistory, error)
}

func (s *InvestmentNAVServer) GetTrackerHistory(ctx context.Context, req *pb.GetTrackerHistoryRequest) (*pb.GetTrackerHistoryResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	q, e := nav.NormalizeTrackerHistory(nav.TrackerHistoryRequest{CompactIssues: req.GetCompactIssues(), PageDays: int(req.GetPageDays()), DetailDate: req.GetDetailDate(), LedgerIDs: req.GetLedgerIds(), Currency: req.GetCurrency(), FXPolicyKey: req.GetFxPolicyKey(), From: req.GetFromDate(), To: req.GetToDate(), PerformanceFrom: req.GetPerformanceFromDate(), GroupIDs: req.GetGroupIds(), InstrumentKeys: req.GetInstrumentKeys(), MonetaryOnly: req.GetMonetaryOnly()}, time.Now().UTC())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	if q.PerformanceFrom != "" && s.trackerSigner == nil {
		return nil, status.Error(codes.Unavailable, "portfolio performance unavailable")
	}
	repo, ok := s.repo.(trackerHistoryRepo)
	if !ok {
		return nil, status.Error(codes.Unavailable, "portfolio history unavailable")
	}
	result, e := repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetTrackerHistoryResponse{AnalyticsInstrumentKeys: result.AnalyticsInstrumentKeys, AnalyticsExcludedKeys: result.AnalyticsExcludedKeys, DetailsIncluded: q.DetailDate != "", InstrumentKeys: q.InstrumentKeys, GroupIds: q.GroupIDs, MonetaryOnly: q.MonetaryOnly, ClassificationVersion: result.ClassificationVersion, Currency: result.Currency, BasisKey: result.BasisKey, FxPolicyKey: result.FXPolicyKey, FxPolicyVersion: result.FXPolicyVersion, FxMode: result.FXMode, NextDate: result.NextDate, Generation: result.Generation, ReadAt: optionalInvestmentTimestamp(result.ReadAt), PriceConvention: md.RetrospectiveCloseNextUTCDay, ReportingTimezone: "Europe/Moscow"}
	for _, p := range result.Points {
		row := &pb.TrackerHistoryPoint{IssueCount: int32(p.IssueCount), Date: p.Date, State: p.State, CoverageKey: p.CoverageKey, KnownValue: protoPerformanceAmount(p.KnownValue), PricedPositions: int32(p.PricedPositions), Positions: int32(p.Positions)}
		for _, issue := range p.Issues {
			row.Issues = append(row.Issues, &pb.InvestmentNAVIssue{ComponentId: issue.ComponentID, Code: issue.Code})
		}
		for _, detail := range p.Details {
			row.Details = append(row.Details, &pb.TrackerValueDetail{LedgerId: detail.LedgerID, LedgerName: detail.LedgerName, InstrumentKey: detail.InstrumentKey, NativeCurrency: detail.Currency, Quantity: protoPerformanceAmount(detail.Quantity), UnitPrice: protoPerformanceAmount(detail.Price), NativeValue: protoPerformanceAmount(detail.NativeValue), Value: protoPerformanceAmount(detail.Value), FxRate: protoPerformanceAmount(detail.FXRate), PriceAt: optionalInvestmentTimestamp(detail.PriceAt), PriceSource: detail.Source, MappingVersion: detail.MappingVersion, Issues: detail.Issues})
		}
		currencies := []string{}
		for currency := range p.DetailFX {
			currencies = append(currencies, currency)
		}
		sort.Strings(currencies)
		for _, currency := range currencies {
			for _, leg := range p.DetailFX[currency] {
				var observed *big.Rat
				for _, fixing := range p.DetailFixings {
					if fixing.ID == leg.ID && fixing.Revision == leg.Revision && fixing.Policy == leg.Policy {
						observed, _ = new(big.Rat).SetString(fixing.Rate)
						break
					}
				}
				row.FxLegs = append(row.FxLegs, &pb.InvestmentCurrentFXLeg{ObservedRate: protoPerformanceAmount(observed), NativeCurrency: currency, Id: leg.ID, Revision: leg.Revision, FromCurrency: leg.From, ToCurrency: leg.To, Policy: leg.Policy, Inverse: leg.Inverse, AsOf: optionalInvestmentTimestamp(leg.At), AvailableAt: optionalInvestmentTimestamp(leg.AvailableAt), ValidUntil: optionalInvestmentTimestamp(leg.ValidUntil)})
			}
		}
		out.Points = append(out.Points, row)
	}
	if q.PerformanceFrom != "" {
		token, err := s.trackerSigner.Seal(tenant, actor, q, result)
		if err != nil {
			return nil, status.Error(codes.Internal, "portfolio snapshot unavailable")
		}
		out.PerformanceSnapshot = token
	}
	return out, nil
}
