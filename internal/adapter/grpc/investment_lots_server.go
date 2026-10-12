package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type investmentLotReader interface {
	InvestmentLots(context.Context, string, string, string, string, string, string, []string) (nav.LotDetails, error)
}

func protoExactLot(lot *inv.RemainingLot) *pb.InvestmentExactLot {
	if lot == nil {
		return nil
	}
	return &pb.InvestmentExactLot{OriginId: lot.OriginID, LotId: lot.LotID, OriginLedgerId: lot.OriginLedgerID, OriginSequence: lot.OriginSequence, AcquiredAt: optionalInvestmentTimestamp(lot.AcquiredAt), Quantity: protoPerformanceAmount(lot.Quantity), CleanCost: protoPerformanceAmount(lot.CleanCost), AcquiredAccrued: protoPerformanceAmount(lot.AcquiredAccrued)}
}
func (s *InvestmentNAVServer) GetInvestmentLots(ctx context.Context, req *pb.GetInvestmentLotsRequest) (*pb.GetInvestmentLotsResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	repo, ok := s.repo.(investmentLotReader)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "lot reader unavailable")
	}
	v, e := repo.InvestmentLots(ctx, tenant, actor, req.GetSeriesKey(), req.GetDate(), req.GetInstrumentKey(), req.GetCursor(), req.GetGroupIds())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentLotsResponse{Date: v.Date, Currency: v.Currency, FxPolicyKey: v.FXPolicyKey, Total: int32(v.Total), NextCursor: v.NextCursor}
	for _, r := range v.Rows {
		item := &pb.InvestmentLotDetail{LedgerId: r.LedgerID, LedgerName: r.LedgerName, BookCurrency: r.Currency, Lot: protoExactLot(&r.Lot), HistoricalCleanCost: protoPerformanceAmount(r.HistoricalCost.Clean), HistoricalAcquiredAccrued: protoPerformanceAmount(r.HistoricalCost.Accrued), HistoricalCleanComplete: r.HistoricalCost.CleanComplete, HistoricalAccruedComplete: r.HistoricalCost.AccruedComplete, Reasons: r.HistoricalCost.Reasons, AcquisitionFxCutoff: optionalInvestmentTimestamp(r.FXAt)}
		if r.Origin.ID != "" {
			item.Origin = protoInvestmentOperation(r.Origin)
		}
		if r.FX != nil {
			item.AcquisitionFxRate = protoPerformanceAmount(r.FX.Rate)
			for _, l := range r.FX.Legs {
				item.FxLegs = append(item.FxLegs, &pb.InvestmentCurrentFXLeg{Id: l.ID, Revision: l.Revision, FromCurrency: l.From, ToCurrency: l.To, Policy: l.Policy, Inverse: l.Inverse, AsOf: optionalInvestmentTimestamp(l.At), AvailableAt: optionalInvestmentTimestamp(l.AvailableAt), ValidUntil: optionalInvestmentTimestamp(l.ValidUntil)})
			}
		}
		for _, c := range r.Changes {
			item.Changes = append(item.Changes, &pb.InvestmentLotChange{LedgerId: c.LedgerID, LedgerName: c.LedgerName, Sequence: c.Sequence, OperationId: c.OperationID, Kind: c.Kind, InstrumentKey: c.InstrumentKey, At: optionalInvestmentTimestamp(c.At), Before: protoExactLot(c.Before), After: protoExactLot(c.After)})
		}
		out.Lots = append(out.Lots, item)
	}
	return out, nil
}
