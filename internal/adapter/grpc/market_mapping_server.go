package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func protoInvestmentMapping(m md.Mapping) *pb.InvestmentMarketMapping {
	out := &pb.InvestmentMarketMapping{InstrumentKey: m.InstrumentKey, Version: m.Version, ActorId: m.ActorID, Reason: m.Reason, RecordedAt: optionalMarketTime(m.RecordedAt)}
	periods := func(input []md.MappingPeriod) []*pb.InvestmentMarketPeriod {
		result := make([]*pb.InvestmentMarketPeriod, 0, len(input))
		for _, p := range input {
			result = append(result, &pb.InvestmentMarketPeriod{Source: &pb.MarketSourceKey{Provider: p.Provider, AccessScope: p.AccessScope}, InstrumentId: p.InstrumentID, Kind: p.Kind, Currency: p.Currency, Unit: p.Unit, Nominal: p.Nominal, NominalCurrency: p.NominalCurrency, LotSize: p.LotSize, From: optionalMarketTime(p.From), To: optionalMarketTime(p.To), Evidence: p.Evidence})
		}
		return result
	}
	out.Periods = periods(m.Periods)
	out.HistoricalPeriods = periods(m.HistoricalPeriods)
	return out
}
func (s *MarketDataServer) GetInvestmentMarketMapping(ctx context.Context, req *pb.GetInvestmentMarketMappingRequest) (*pb.GetInvestmentMarketMappingResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	if s.mappings == nil {
		return nil, status.Error(codes.Unavailable, "market mappings unavailable")
	}
	m, e := s.mappings.Get(ctx, t, u, req.GetInstrumentKey())
	if e != nil {
		return nil, marketRPCError(e)
	}
	return &pb.GetInvestmentMarketMappingResponse{Mapping: protoInvestmentMapping(m)}, nil
}
func (s *MarketDataServer) SaveInvestmentMarketMapping(ctx context.Context, req *pb.SaveInvestmentMarketMappingRequest) (*pb.SaveInvestmentMarketMappingResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	if s.mappings == nil {
		return nil, status.Error(codes.Unavailable, "market mappings unavailable")
	}
	if len(req.GetPeriods()) > 100 || len(req.GetHistoricalPeriods()) > 100 || !req.GetReplaceHistorical() && len(req.GetHistoricalPeriods()) > 0 {
		return nil, marketRPCError(md.ErrInvalid)
	}
	c := md.MappingChange{InstrumentKey: req.GetInstrumentKey(), ExpectedVersion: req.GetExpectedVersion(), RequestKey: req.GetRequestKey(), Reason: req.GetReason()}
	parsePeriods := func(input []*pb.InvestmentMarketPeriod) ([]md.MappingPeriod, error) {
		result := make([]md.MappingPeriod, 0, len(input))
		for _, p := range input {
			from, err := assetTimestamp(p.GetFrom())
			if err != nil {
				return nil, err
			}
			period := md.MappingPeriod{Provider: p.GetSource().GetProvider(), AccessScope: p.GetSource().GetAccessScope(), InstrumentID: p.GetInstrumentId(), Kind: p.GetKind(), Currency: p.GetCurrency(), Unit: p.GetUnit(), Nominal: p.GetNominal(), NominalCurrency: p.GetNominalCurrency(), LotSize: p.GetLotSize(), From: from, Evidence: p.GetEvidence()}
			if p.GetTo() != nil {
				period.To, err = assetTimestamp(p.GetTo())
				if err != nil {
					return nil, err
				}
			}
			result = append(result, period)
		}
		return result, nil
	}
	c.Periods, e = parsePeriods(req.GetPeriods())
	if e != nil {
		return nil, e
	}
	if req.GetReplaceHistorical() {
		periods, err := parsePeriods(req.GetHistoricalPeriods())
		if err != nil {
			return nil, err
		}
		c.HistoricalPeriods = &periods
	}
	m, e := s.mappings.Save(ctx, t, u, c)
	if e != nil {
		return nil, marketRPCError(e)
	}
	return &pb.SaveInvestmentMarketMappingResponse{Mapping: protoInvestmentMapping(m)}, nil
}
