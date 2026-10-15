package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

func (s *InvestmentNAVServer) GetTrackerInstrumentPrices(ctx context.Context, req *pb.GetTrackerInstrumentPricesRequest) (*pb.GetTrackerInstrumentPricesResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	q, e := nav.NormalizeTrackerInstrument(nav.TrackerInstrumentRequest{InstrumentKey: req.GetInstrumentKey(), Currency: req.GetCurrency(), FXPolicyKey: req.GetFxPolicyKey(), From: req.GetFromDate(), To: req.GetToDate(), ListingID: req.GetListingId(), Provider: req.GetProvider(), AccessScope: req.GetAccessScope(), InstrumentID: req.GetInstrumentId()}, time.Now().UTC())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	repo, ok := s.repo.(interface {
		TrackerInstrumentPrices(context.Context, string, string, nav.TrackerInstrumentRequest) (nav.TrackerInstrumentHistory, error)
	})
	if !ok {
		return nil, status.Error(codes.Unavailable, "instrument history unavailable")
	}
	result, e := repo.TrackerInstrumentPrices(ctx, tenant, actor, q)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetTrackerInstrumentPricesResponse{InstrumentKey: result.InstrumentKey, Currency: result.Currency, FxPolicyKey: result.FXPolicyKey, FxPolicyVersion: result.FXPolicyVersion, FxMode: result.FXMode, NextDate: result.NextDate, BasisKey: result.BasisKey, ReadAt: optionalInvestmentTimestamp(result.ReadAt), MetadataAt: optionalInvestmentTimestamp(result.MetadataAt), ListingOnly: result.ListingOnly, BondLimited: result.BondLimited, MetadataInstrumentId: result.MetadataInstrumentID, MetadataEnabled: result.MetadataEnabled, Source: protoMarketKey(result.Scope), PriceConvention: md.RetrospectiveCloseNextUTCDay, ReportingTimezone: "Europe/Moscow"}
	if result.Instrument != nil {
		out.Instrument = positionMetadata(*result.Instrument)
	}
	if result.Quote != nil {
		q := result.Quote
		out.Quote = &pb.InvestmentQuote{InstrumentKey: q.InstrumentKey, Currency: q.Currency, CleanPrice: protoPerformanceAmount(q.CleanPrice), PriceAt: optionalInvestmentTimestamp(q.At), Source: q.Source, State: q.State, Issues: q.Issues, MappingVersion: q.MappingVersion}
	}
	if result.Bond != nil {
		out.Bond = protoBondFacts(result.Bond, result.Scope)
	}
	for _, l := range result.Listings {
		p := l.Period
		out.Listings = append(out.Listings, &pb.TrackerInstrumentListing{Id: l.ID, Source: &pb.MarketSourceKey{Provider: p.Provider, AccessScope: p.AccessScope}, InstrumentId: p.InstrumentID, Kind: p.Kind, Currency: p.Currency, Unit: p.Unit, From: optionalInvestmentTimestamp(p.From), To: optionalInvestmentTimestamp(p.To), Enabled: l.Enabled})
	}
	if result.ListingOnly {
		for _, l := range out.Listings {
			l.From = nil
			l.To = nil
		}
	}
	for _, p := range result.Points {
		out.Points = append(out.Points, &pb.TrackerInstrumentPricePoint{Date: p.Date, State: p.State, NativeCurrency: p.NativeCurrency, Unit: p.Unit, Raw: protoPerformanceAmount(p.Raw), NativeClean: protoPerformanceAmount(p.NativeClean), Clean: protoPerformanceAmount(p.Clean), Accrued: protoPerformanceAmount(p.Accrued), NativeDirty: protoPerformanceAmount(p.NativeDirty), Dirty: protoPerformanceAmount(p.Dirty), Source: p.Source, ObservationId: p.ObservationID, ObservationRevision: p.Revision, CandleDate: p.CandleDate, Nominal: p.Nominal, NominalCurrency: p.NominalCurrency, Issues: p.Issues})
	}
	return out, nil
}
