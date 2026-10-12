package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func (s *MarketDataServer) GetMarketDividendFacts(ctx context.Context, req *pb.GetMarketDividendFactsRequest) (*pb.GetMarketDividendFactsResponse, error) {
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
	f, err := s.svc.DividendFacts(ctx, t, u, scope, md.JobRequest{Kind: "dividend_facts", InstrumentID: req.GetInstrumentId(), From: from, To: to})
	if err != nil {
		return nil, marketRPCError(err)
	}
	out := &pb.GetMarketDividendFactsResponse{Source: protoMarketKey(scope), CoverageState: "unavailable"}
	if f == nil {
		return out, nil
	}
	out.CoverageState = "observations_only"
	out.SnapshotId = f.ID
	out.PayloadHash = f.Hash
	out.RecordedAt = optionalMarketTime(f.RecordedAt)
	out.AsOf = optionalMarketTime(f.AsOf)
	out.From = optionalMarketTime(f.From)
	out.To = optionalMarketTime(f.To)
	i := f.Instrument
	out.Instrument = &pb.MarketInstrument{InstrumentId: i.ID, Figi: i.FIGI, Isin: i.ISIN, Ticker: i.Ticker, ClassCode: i.ClassCode, Name: i.Name, Kind: i.Kind, Currency: i.Currency, Detailed: i.Detailed}
	for _, d := range f.Dividends {
		out.Dividends = append(out.Dividends, &pb.MarketDividend{DividendNet: protoBondAmount(d.DividendNet), ClosePrice: protoBondAmount(d.ClosePrice), PaymentDate: optionalMarketTime(d.PaymentDate), DeclaredDate: optionalMarketTime(d.DeclaredDate), LastBuyDate: optionalMarketTime(d.LastBuyDate), RecordDate: optionalMarketTime(d.RecordDate), CreatedAt: optionalMarketTime(d.CreatedAt), DividendType: d.DividendType, Regularity: d.Regularity, YieldValue: d.YieldValue})
	}
	return out, nil
}
