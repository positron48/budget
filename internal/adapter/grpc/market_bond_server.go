package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func protoBondAmount(v *md.BondAmount) *pb.MarketBondAmount {
	if v == nil {
		return nil
	}
	return &pb.MarketBondAmount{Amount: v.Amount, Currency: v.Currency}
}
func protoBondFacts(f *md.SavedBondFacts, scope md.Scope) *pb.GetMarketBondFactsResponse {
	out := &pb.GetMarketBondFactsResponse{Source: protoMarketKey(scope), CoverageState: "unavailable"}
	if f == nil {
		return out
	}
	out.CoverageState = "observations_only"
	out.SnapshotId = f.ID
	out.PayloadHash = f.Hash
	out.RecordedAt = optionalMarketTime(f.RecordedAt)
	out.AsOf = optionalMarketTime(f.AsOf)
	out.From = optionalMarketTime(f.From)
	out.To = optionalMarketTime(f.To)
	out.InstrumentId = f.Instrument.ID
	t := f.Terms
	out.Terms = &pb.MarketBondTerms{Nominal: protoBondAmount(t.Nominal), InitialNominal: protoBondAmount(t.InitialNominal), CurrentAccrued: protoBondAmount(t.CurrentAccrued), Maturity: optionalMarketTime(t.Maturity), CallDate: optionalMarketTime(t.CallDate), Placement: optionalMarketTime(t.Placement), Floating: t.Floating, Perpetual: t.Perpetual, Amortizing: t.Amortizing, CouponFrequency: t.CouponFrequency}
	for _, p := range f.Coupons {
		out.Coupons = append(out.Coupons, &pb.MarketBondCoupon{Number: p.Number, Date: optionalMarketTime(p.Date), FixDate: optionalMarketTime(p.FixDate), Start: optionalMarketTime(p.Start), End: optionalMarketTime(p.End), Kind: p.Kind, Pay: protoBondAmount(p.Pay), PeriodDays: p.PeriodDays})
	}
	for _, p := range f.Events {
		out.Events = append(out.Events, &pb.MarketBondEvent{Number: p.Number, Kind: p.Kind, Date: optionalMarketTime(p.Date), FixDate: optionalMarketTime(p.FixDate), PayDate: optionalMarketTime(p.PayDate), ActualPayDate: optionalMarketTime(p.ActualPayDate), Start: optionalMarketTime(p.Start), End: optionalMarketTime(p.End), Pay: protoBondAmount(p.Pay), Value: p.Value, AnnualRate: p.AnnualRate, Execution: p.Execution, OperationType: p.OperationType, Note: p.Note, ConvertTo: p.ConvertTo})
	}
	for _, p := range f.Accrued {
		out.Accrued = append(out.Accrued, &pb.MarketBondAccrued{Date: optionalMarketTime(p.Date), Value: p.Value, Percent: p.Percent, Nominal: p.Nominal})
	}
	return out
}
func (s *MarketDataServer) GetMarketBondFacts(ctx context.Context, req *pb.GetMarketBondFactsRequest) (*pb.GetMarketBondFactsResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	from, err := assetTimestamp(req.GetFrom())
	if err != nil {
		return nil, err
	}
	to, err := assetTimestamp(req.GetTo())
	if err != nil {
		return nil, err
	}
	scope := marketScope(t, req.GetSource())
	f, err := s.svc.BondFacts(ctx, t, u, scope, md.JobRequest{Kind: "bond_facts", InstrumentID: req.GetInstrumentId(), From: from, To: to})
	if err != nil {
		return nil, marketRPCError(err)
	}
	return protoBondFacts(f, scope), nil
}
