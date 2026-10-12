package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

func protoNAVVariant(v nav.Variant) *pb.InvestmentNAVVariantResponse {
	s := v.Series
	return &pb.InvestmentNAVVariantResponse{Series: &pb.InvestmentNAVSeries{Key: s.Key, LedgerIds: s.LedgerIDs, Currency: s.Currency, FxPolicyKey: s.FXPolicyKey, FromDate: s.From, ToDate: s.To, ExtensionLimited: s.ExtensionLimited}, Registered: v.Registered, QueuedDays: int32(v.QueuedDays)}
}
func (s *InvestmentNAVServer) GetInvestmentNAVVariant(ctx context.Context, req *pb.GetInvestmentNAVVariantRequest) (*pb.GetInvestmentNAVVariantResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.repo.NAVVariant(ctx, tenant, actor, req.GetSourceSeriesKey(), req.GetCurrency(), req.GetFxPolicyKey())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := protoNAVVariant(v)
	return &pb.GetInvestmentNAVVariantResponse{Series: out.Series, Registered: out.Registered, QueuedDays: out.QueuedDays}, nil
}
func (s *InvestmentNAVServer) PrepareInvestmentNAVVariant(ctx context.Context, req *pb.PrepareInvestmentNAVVariantRequest) (*pb.PrepareInvestmentNAVVariantResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.repo.PrepareNAVVariant(ctx, tenant, actor, req.GetSourceSeriesKey(), req.GetCurrency(), req.GetFxPolicyKey())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := protoNAVVariant(v)
	return &pb.PrepareInvestmentNAVVariantResponse{Series: out.Series, Registered: out.Registered, QueuedDays: out.QueuedDays}, nil
}
