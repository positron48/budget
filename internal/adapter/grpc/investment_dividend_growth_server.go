package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

func dividendTimestamp(t *timestamppb.Timestamp) (time.Time, error) {
	if t == nil || t.CheckValid() != nil {
		return time.Time{}, d.ErrInvestmentInvalid
	}
	return t.AsTime(), nil
}
func parseDividendHistory(p *pb.InvestmentDividendHistory) (inv.DividendHistory, error) {
	h := inv.DividendHistory{}
	if p == nil {
		return h, d.ErrInvestmentInvalid
	}
	h = inv.DividendHistory{ID: p.Id, InstrumentKey: p.InstrumentKey, Currency: p.Currency, Timezone: p.Timezone, SourceReference: p.SourceReference, Evidence: p.Evidence, Reason: p.Reason, Version: p.Version, Cancelled: p.Cancelled}
	var e error
	h.BasisAt, e = dividendTimestamp(p.BasisAt)
	if e != nil {
		return h, e
	}
	h.ReviewedFrom, e = dividendTimestamp(p.ReviewedFrom)
	if e != nil {
		return h, e
	}
	for _, v := range p.Years {
		if v == nil {
			return h, d.ErrInvestmentInvalid
		}
		h.Years = append(h.Years, inv.DividendYearReview{Year: int(v.Year), Complete: v.Complete, SourceReference: v.SourceReference, Evidence: v.Evidence})
	}
	for _, v := range p.Dividends {
		if v == nil {
			return h, d.ErrInvestmentInvalid
		}
		at, err := dividendTimestamp(v.UnitBasisAt)
		if err != nil {
			return h, err
		}
		h.Dividends = append(h.Dividends, inv.DividendDeclaration{ID: v.Id, PaymentDate: v.PaymentDate, Amount: v.Amount, Class: v.Class, Status: v.Status, UnitBasisAt: at, SourceReference: v.SourceReference, Evidence: v.Evidence})
	}
	for _, v := range p.Splits {
		if v == nil {
			return h, d.ErrInvestmentInvalid
		}
		at, err := dividendTimestamp(v.EffectiveAt)
		if err != nil {
			return h, err
		}
		h.Splits = append(h.Splits, inv.DividendUnitSplit{ID: v.Id, NewUnits: v.NewUnits, OldUnits: v.OldUnits, EffectiveAt: at, SourceReference: v.SourceReference, Evidence: v.Evidence})
	}
	return inv.NormalizeDividendHistory(h)
}
func protoDividendHistory(h inv.DividendHistory) *pb.InvestmentDividendHistory {
	p := &pb.InvestmentDividendHistory{Id: h.ID, InstrumentKey: h.InstrumentKey, Currency: h.Currency, Timezone: h.Timezone, BasisAt: optionalInvestmentTimestamp(h.BasisAt), ReviewedFrom: optionalInvestmentTimestamp(h.ReviewedFrom), SourceReference: h.SourceReference, Evidence: h.Evidence, Reason: h.Reason, Version: h.Version, Cancelled: h.Cancelled}
	for _, v := range h.Years {
		p.Years = append(p.Years, &pb.InvestmentDividendYearReview{Year: int32(v.Year), Complete: v.Complete, SourceReference: v.SourceReference, Evidence: v.Evidence})
	}
	for _, v := range h.Dividends {
		p.Dividends = append(p.Dividends, &pb.InvestmentDividendDeclaration{Id: v.ID, PaymentDate: v.PaymentDate, Amount: v.Amount, Class: v.Class, Status: v.Status, UnitBasisAt: optionalInvestmentTimestamp(v.UnitBasisAt), SourceReference: v.SourceReference, Evidence: v.Evidence})
	}
	for _, v := range h.Splits {
		p.Splits = append(p.Splits, &pb.InvestmentDividendUnitSplit{Id: v.ID, NewUnits: v.NewUnits, OldUnits: v.OldUnits, EffectiveAt: optionalInvestmentTimestamp(v.EffectiveAt), SourceReference: v.SourceReference, Evidence: v.Evidence})
	}
	return p
}
func (s *InvestmentServer) GetInvestmentDividendGrowth(ctx context.Context, _ *pb.GetInvestmentDividendGrowthRequest) (*pb.GetInvestmentDividendGrowthResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	r, e := s.svc.DividendGrowth(ctx, t, u)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentDividendGrowthResponse{AsOf: optionalInvestmentTimestamp(r.AsOf)}
	for _, v := range r.Histories {
		p := &pb.InvestmentDividendHistoryReport{History: protoDividendHistory(v.History)}
		for _, y := range v.Years {
			p.Years = append(p.Years, &pb.InvestmentDividendYearGrowth{Year: int32(y.Year), Regular: protoPayoutAmount(y.Regular), Special: protoPayoutAmount(y.Special), Total: protoPayoutAmount(y.Total), KnownTotal: protoPayoutAmount(y.KnownTotal), PreviousRegular: protoPayoutAmount(y.PreviousRegular), Change: protoPayoutAmount(y.Change), PercentChange: protoPayoutAmount(y.PercentChange), SpecialCount: int32(y.SpecialCount), UnknownClassCount: int32(y.UnknownClassCount), Status: y.Status})
		}
		out.Histories = append(out.Histories, p)
	}
	return out, nil
}
func (s *InvestmentServer) SaveInvestmentDividendHistory(ctx context.Context, req *pb.SaveInvestmentDividendHistoryRequest) (*pb.SaveInvestmentDividendHistoryResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	h, e := parseDividendHistory(req.GetHistory())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	h, e = s.svc.SaveDividendHistory(ctx, t, u, h, req.GetExpectedVersion())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.SaveInvestmentDividendHistoryResponse{History: protoDividendHistory(h)}, nil
}
