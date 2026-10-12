package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

func protoLotConversion(v *d.InvestmentLotConversion) *pb.InvestmentLotConversion {
	if v == nil {
		return nil
	}
	return &pb.InvestmentLotConversion{Lots: v.Lots, LotSize: v.LotSize, TermsId: v.TermsID, MappingVersion: v.MappingVersion, Provider: v.Provider, AccessScope: v.AccessScope, InstrumentId: v.InstrumentID, Evidence: v.Evidence, From: optionalInvestmentTimestamp(v.From), To: optionalInvestmentTimestamp(v.To)}
}
func parseLotConversion(v *pb.InvestmentLotConversion) (*d.InvestmentLotConversion, error) {
	if v == nil {
		return nil, nil
	}
	out := &d.InvestmentLotConversion{Lots: v.GetLots(), LotSize: v.GetLotSize(), TermsID: v.GetTermsId(), MappingVersion: v.GetMappingVersion(), Provider: v.GetProvider(), AccessScope: v.GetAccessScope(), InstrumentID: v.GetInstrumentId(), Evidence: v.GetEvidence()}
	if v.GetFrom() != nil {
		if e := v.GetFrom().CheckValid(); e != nil {
			return nil, d.ErrInvestmentInvalid
		}
		out.From = v.GetFrom().AsTime()
	}
	if v.GetTo() != nil {
		if e := v.GetTo().CheckValid(); e != nil {
			return nil, d.ErrInvestmentInvalid
		}
		out.To = v.GetTo().AsTime()
	}
	return out, nil
}
func (s *InvestmentServer) PreviewInvestmentLotQuantity(ctx context.Context, req *pb.PreviewInvestmentLotQuantityRequest) (*pb.PreviewInvestmentLotQuantityResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	var at time.Time
	var timing *d.InvestmentOperationTiming
	if v := req.GetTiming(); v != nil {
		timing = &d.InvestmentOperationTiming{Timezone: v.GetTimezone(), OccurredDate: v.GetOccurredDate()}
	}
	if req.GetOccurredAt() != nil || timing == nil || timing.OccurredDate == "" {
		at, e = assetTimestamp(req.GetOccurredAt())
		if e != nil {
			return nil, e
		}
	}
	out, e := s.svc.PreviewLotQuantity(ctx, t, u, inv.LotQuantityRequest{InstrumentKey: req.GetInstrumentKey(), Lots: req.GetLots(), OccurredAt: at, Timing: timing})
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.PreviewInvestmentLotQuantityResponse{Quantity: out.Quantity, Conversion: protoLotConversion(out.Conversion), EffectiveAt: timestamppb.New(out.EffectiveAt)}, nil
}
