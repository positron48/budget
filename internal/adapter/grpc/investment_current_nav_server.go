package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"math/big"
	"sort"
)

func (s *InvestmentNAVServer) GetInvestmentCurrentNAV(ctx context.Context, req *pb.GetInvestmentCurrentNAVRequest) (*pb.GetInvestmentCurrentNAVResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.repo.CurrentNAVForSeries(ctx, tenant, actor, req.GetSeriesKey())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return protoCurrentNAV(v), nil
}
func protoCurrentNAV(v nav.CurrentNAV) *pb.GetInvestmentCurrentNAVResponse {
	r := v.Result
	out := &pb.GetInvestmentCurrentNAVResponse{Provisional: true, SeriesKey: v.SeriesKey, Currency: r.Currency, AsOf: optionalInvestmentTimestamp(r.At), LastCompletedDate: v.LastCompletedDate, Complete: r.Complete, KnownValue: protoPerformanceAmount(r.KnownValue), Generation: r.DataGeneration, PriceProfileKey: r.ConventionKey, PriceProfileVersion: r.ConventionVersion, FxPolicy: r.FXPolicy}
	for id := range r.LedgerVersions {
		out.LedgerIds = append(out.LedgerIds, id)
	}
	sort.Strings(out.LedgerIds)
	for _, i := range r.Issues {
		out.Issues = append(out.Issues, &pb.InvestmentNAVIssue{ComponentId: i.ComponentID, Code: i.Code})
	}
	for _, i := range r.Warnings {
		out.Warnings = append(out.Warnings, &pb.InvestmentNAVIssue{ComponentId: i.ComponentID, Code: i.Code})
	}
	for _, q := range v.Quotes {
		item := &pb.InvestmentCurrentQuote{InstrumentKey: q.InstrumentKey, State: q.Price.State, Provider: q.Scope.Provider, AccessScope: q.Scope.AccessScope, SourceKind: q.SourceKind, Adjustment: q.Adjustment, ReceivedAt: optionalInvestmentTimestamp(q.Price.ReceivedAt), ObservationId: q.Price.ObservationID, Revision: q.Price.ObservationRevision, Issues: q.Price.Issues}
		if c := q.Price.Candidate; c != nil {
			item.Currency = c.Currency
			item.Unit = c.Unit
			item.PriceAt = optionalInvestmentTimestamp(c.EffectiveAt)
			item.ValidUntil = optionalInvestmentTimestamp(c.EffectiveAt.Add(q.MaxAge))
			if amount, ok := new(big.Rat).SetString(c.Price); ok {
				item.CandidatePrice = protoPerformanceAmount(amount)
			}
		}
		out.Quotes = append(out.Quotes, item)
	}
	ids := make([]string, 0, len(r.FXPaths))
	for id := range r.FXPaths {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, leg := range r.FXPaths[id] {
			var observed *big.Rat
			for _, fixing := range v.FXFixings {
				if fixing.ID == leg.ID && fixing.Revision == leg.Revision && fixing.Policy == leg.Policy {
					observed, _ = new(big.Rat).SetString(fixing.Rate)
					break
				}
			}
			out.FxLegs = append(out.FxLegs, &pb.InvestmentCurrentFXLeg{ObservedRate: protoPerformanceAmount(observed), NativeCurrency: id, Id: leg.ID, Revision: leg.Revision, FromCurrency: leg.From, ToCurrency: leg.To, Policy: leg.Policy, Inverse: leg.Inverse, AsOf: optionalInvestmentTimestamp(leg.At), AvailableAt: optionalInvestmentTimestamp(leg.AvailableAt), ValidUntil: optionalInvestmentTimestamp(leg.ValidUntil)})
		}
	}
	return out
}
