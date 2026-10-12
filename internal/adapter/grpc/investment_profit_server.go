package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

func (s *InvestmentNAVServer) GetInvestmentProfit(ctx context.Context, req *pb.GetInvestmentProfitRequest) (*pb.GetInvestmentProfitResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	value, e := s.repo.ProfitForSeries(ctx, tenant, actor, req.GetSeriesKey(), req.GetFromDate(), req.GetToDate())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	p := value.Performance
	out := &pb.GetInvestmentProfitResponse{SeriesKey: p.SeriesKey, FromDate: p.From, ToDate: p.To, Currency: p.Currency, FxPolicyKey: p.FXPolicyKey, Generation: p.Generation, Reason: p.Reason, AttributionComplete: value.Attribution.Complete, AttributionReason: value.Attribution.Reason, Convention: "nav_minus_external_flows_native_event_pnl_boundary_unrealized_cash_fx_residual"}
	if p.Reason == "" {
		out.Profit = protoPerformanceAmount(p.Profit)
	}
	for _, point := range value.Timeline.Points {
		item := &pb.InvestmentProfitPoint{Date: point.Date, DailyReason: point.DailyReason, CumulativeReason: point.CumulativeReason}
		if point.DailyReason == "" {
			item.Daily = protoPerformanceAmount(point.Daily)
		}
		if point.CumulativeReason == "" {
			item.Cumulative = protoPerformanceAmount(point.Cumulative)
		}
		out.Points = append(out.Points, item)
	}
	buckets := func(values []calc.ProfitBucket) []*pb.InvestmentProfitBucket {
		items := []*pb.InvestmentProfitBucket{}
		for _, b := range values {
			item := &pb.InvestmentProfitBucket{Period: b.Period, FromDate: b.From, ToDate: b.To, Reason: b.Reason, NetContributions: protoPerformanceAmount(b.NetContributions)}
			if b.Reason == "" {
				item.Profit = protoPerformanceAmount(b.Profit)
			}
			items = append(items, item)
		}
		return items
	}
	out.Months = buckets(value.Timeline.Months)
	out.Years = buckets(value.Timeline.Years)
	for _, c := range value.Attribution.Components {
		out.Components = append(out.Components, &pb.InvestmentProfitComponent{Kind: c.Kind, Known: protoPerformanceAmount(c.Known), Complete: c.Complete, Reason: c.Reason, Informational: c.Informational})
	}
	out.InstrumentsComplete = value.Instruments.Complete && p.Reason == ""
	if out.InstrumentsComplete {
		out.InstrumentRemainderShare = protoPerformanceAmount(value.Instruments.RemainderShare)
	}
	out.InstrumentConvention = "closing_market_minus_opening_market_plus_dated_instrument_economic_cash"
	c := value.Instruments.Remainder
	out.InstrumentRemainder = &pb.InvestmentProfitComponent{Kind: c.Kind, Complete: out.InstrumentsComplete && c.Complete, Reason: c.Reason}
	if out.InstrumentRemainder.Complete {
		out.InstrumentRemainder.Known = protoPerformanceAmount(c.Known)
	}
	for _, row := range value.Instruments.Rows {
		item := &pb.InvestmentInstrumentContribution{InstrumentKey: row.InstrumentKey, LedgerIds: row.LedgerIDs, Closed: row.Closed, Known: protoPerformanceAmount(row.Known), Complete: row.Complete, Metadata: positionMetadata(row.Metadata)}
		if out.InstrumentsComplete && row.Complete {
			item.Share = protoPerformanceAmount(row.Share)
		}
		for _, c := range row.Components {
			item.Components = append(item.Components, &pb.InvestmentProfitComponent{Kind: c.Kind, Known: protoPerformanceAmount(c.Known), Complete: c.Complete, Reason: c.Reason})
		}
		out.Instruments = append(out.Instruments, item)
	}
	return out, nil
}
