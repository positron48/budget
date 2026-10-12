package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func positionMetadata(i md.Instrument) *pb.MarketInstrument {
	if i.ID == "" {
		return nil
	}
	return &pb.MarketInstrument{InstrumentId: i.ID, Figi: i.FIGI, Isin: i.ISIN, Ticker: i.Ticker, ClassCode: i.ClassCode, Name: i.Name, Kind: i.Kind, Currency: i.Currency, Detailed: i.Detailed}
}
func protoPositionDetails(v nav.PositionDetails) *pb.GetInvestmentPositionsResponse {
	out := &pb.GetInvestmentPositionsResponse{Date: v.Date, Currency: v.Currency, FxPolicyKey: v.FXPolicyKey, ValuationState: v.ValuationState, ClassificationVersion: v.Classification.Version}
	for _, row := range v.Rows {
		p := row.Position
		item := &pb.InvestmentPositionDetail{HistoricalCleanCost: protoPerformanceAmount(row.HistoricalCost.Clean), HistoricalAcquiredAccrued: protoPerformanceAmount(row.HistoricalCost.Accrued), HistoricalCleanComplete: row.HistoricalCost.CleanComplete, HistoricalAccruedComplete: row.HistoricalCost.AccruedComplete, HistoricalCostReasons: row.HistoricalCost.Reasons, LedgerId: row.LedgerID, LedgerName: row.LedgerName, InstrumentKey: p.InstrumentKey, GroupId: row.GroupID, Quantity: p.Quantity, BookCurrency: p.Currency, CleanCost: p.CleanCost, AcquiredAccrued: p.AcquiredAccrued, RealizedClean: p.RealizedClean, RealizedAccrued: p.RealizedAccrued, Value: protoPerformanceAmount(row.Value), Weight: protoPerformanceAmount(row.Weight), UnrealizedClean: protoPerformanceAmount(row.UnrealizedClean), UnrealizedDirty: protoPerformanceAmount(row.UnrealizedDirty), PriceCurrency: row.NativePrice.Currency, CleanPricePerSecurity: protoPerformanceAmount(row.NativePrice.Clean), AccruedPerSecurity: protoPerformanceAmount(row.NativePrice.Accrued), DirtyPricePerSecurity: protoPerformanceAmount(row.NativePrice.Dirty), Metadata: positionMetadata(v.Metadata[p.InstrumentKey])}
		if fact := row.Facts; fact != nil {
			item.Nominal = fact.Terms.Nominal
			item.NominalCurrency = fact.Terms.NominalCurrency
			item.LotSize = fact.Terms.LotSize
			item.InstrumentType = fact.Terms.Kind
			if fact.Price != nil && row.NativePrice.Clean != nil {
				item.ProviderPrice = fact.Price.Price
				item.PriceUnit = fact.Terms.Unit
				item.PriceAt = optionalInvestmentTimestamp(fact.Price.EffectiveAt)
				item.PriceSource = fact.Price.SourceKey
			}
		}
		for _, payment := range row.Payments {
			item.Payments = append(item.Payments, &pb.InvestmentPositionPayment{Currency: payment.Currency, Gross: protoPerformanceAmount(payment.Gross), Tax: protoPerformanceAmount(payment.Tax), Net: protoPerformanceAmount(payment.Net)})
		}
		for _, issue := range row.Issues {
			item.Issues = append(item.Issues, &pb.InvestmentNAVIssue{ComponentId: issue.ComponentID, Code: issue.Code})
		}
		out.Positions = append(out.Positions, item)
	}
	return out
}
func (s *InvestmentNAVServer) GetInvestmentPositions(ctx context.Context, req *pb.GetInvestmentPositionsRequest) (*pb.GetInvestmentPositionsResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	out, e := s.repo.InvestmentPositions(ctx, tenant, actor, req.GetSeriesKey(), req.GetDate(), req.GetGroupIds())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return protoPositionDetails(out), nil
}
func (s *InvestmentNAVServer) GetInvestmentInstrument(ctx context.Context, req *pb.GetInvestmentInstrumentRequest) (*pb.GetInvestmentInstrumentResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.repo.InvestmentInstrument(ctx, tenant, actor, req.GetSeriesKey(), req.GetDate(), req.GetInstrumentKey(), req.GetCursor(), req.GetGroupIds())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentInstrumentResponse{Holdings: protoPositionDetails(v.PositionDetails), InstrumentKey: v.InstrumentKey, AsOf: optionalInvestmentTimestamp(v.AsOf), NextCursor: v.NextCursor, Metadata: positionMetadata(v.Metadata[v.InstrumentKey])}
	for _, o := range v.Operations {
		out.Operations = append(out.Operations, protoInvestmentOperation(o))
	}
	return out, nil
}

func (s *InvestmentNAVServer) GetInvestmentInstrumentPrices(ctx context.Context, req *pb.GetInvestmentInstrumentPricesRequest) (*pb.GetInvestmentInstrumentPricesResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.repo.InvestmentInstrumentPrices(ctx, tenant, actor, req.GetSeriesKey(), req.GetInstrumentKey(), req.GetFromDate(), req.GetToDate(), req.GetGroupIds())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentInstrumentPricesResponse{InstrumentKey: v.InstrumentKey, Currency: v.Currency, FxPolicyKey: v.FXPolicyKey, PriceProfileVersion: v.PriceProfileVersion}
	for _, o := range v.Markers {
		out.Markers = append(out.Markers, protoInvestmentOperation(o))
	}
	for _, p := range v.Days {
		point := &pb.InvestmentInstrumentPricePoint{Date: p.Date, State: p.State, NativeCurrency: p.NativeCurrency, Percent: protoPerformanceAmount(p.Percent), NativeClean: protoPerformanceAmount(p.NativeClean), NativeDirty: protoPerformanceAmount(p.NativeDirty), TargetClean: protoPerformanceAmount(p.TargetClean), TargetDirty: protoPerformanceAmount(p.TargetDirty), PriceAt: optionalInvestmentTimestamp(p.PriceAt), PriceSource: p.PriceSource, Adjustment: p.Adjustment}
		if a := p.AdjustmentProof; a != nil {
			point.AdjustmentEvidence = &pb.InvestmentPriceAdjustmentEvidence{Basis: a.Basis, Nominal: a.Nominal, NominalCurrency: a.NominalCurrency, SourcePrice: a.SourcePrice, Multiplier: a.Multiplier, Divisor: a.Divisor, ResolvedPrice: a.ResolvedPrice, SourceReference: a.SourceReference, EvidenceSha256: a.EvidenceSHA256, ObservationHash: a.ObservationHash, ProfileKey: a.ProfileKey, ProfileVersion: a.ProfileVersion, ObservationId: a.ObservationID, ObservationRevision: a.ObservationRevision}
		}
		if a := p.Accrued; a != nil {
			point.AccruedEvidence = &pb.InvestmentAccruedEvidence{SourceId: a.SourceID, InstrumentId: a.InstrumentID, Currency: a.Currency, AmountPerUnit: a.AmountPerUnit, From: optionalInvestmentTimestamp(a.From), To: optionalInvestmentTimestamp(a.To)}
		}
		for _, i := range p.Issues {
			point.Issues = append(point.Issues, &pb.InvestmentNAVIssue{ComponentId: i.ComponentID, Code: i.Code})
		}
		out.Points = append(out.Points, point)
	}
	return out, nil
}
