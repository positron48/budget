package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math/big"
	"time"
)

func (s *InvestmentNAVServer) GetTrackerContributions(ctx context.Context, req *pb.GetTrackerContributionsRequest) (*pb.GetTrackerContributionsResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	q, e := nav.NormalizeTrackerHistory(nav.TrackerHistoryRequest{LedgerIDs: req.GetLedgerIds(), GroupIDs: req.GetGroupIds(), InstrumentKeys: req.GetInstrumentKeys(), Currency: req.GetCurrency(), FXPolicyKey: req.GetFxPolicyKey(), From: req.GetFromDate(), To: req.GetToDate(), ContributionOnly: true}, time.Now().UTC())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	repo, ok := s.repo.(trackerHistoryRepo)
	if !ok {
		return nil, status.Error(codes.Unavailable, "contribution history unavailable")
	}
	h, e := repo.TrackerHistory(ctx, tenant, actor, q)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	if h.Baseline == nil || len(h.Points) != 1 {
		return nil, status.Error(codes.Internal, "contribution history unavailable")
	}
	out := &pb.GetTrackerContributionsResponse{InstrumentKeys: q.InstrumentKeys, GroupIds: q.GroupIDs, FromDate: q.From, ToDate: q.To, Currency: h.Currency, FxPolicyKey: h.FXPolicyKey, FxPolicyVersion: h.FXPolicyVersion, FxMode: h.FXMode, ClassificationVersion: h.ClassificationVersion, ReadAt: optionalInvestmentTimestamp(h.ReadAt), BasisKey: h.BasisKey, Convention: "recorded_securities_pnl_attribution_v1", KnownProfit: new(pb.InvestmentNAVAmount)}
	sum := new(big.Rat)
	complete := h.Baseline.State == "ready" && h.Points[0].State == "ready"
	for _, r := range h.Contributions {
		if !r.InScope {
			continue
		}
		if r.Profit == nil {
			complete = false
		} else {
			sum.Add(sum, r.Profit)
		}
	}
	out.KnownProfit = protoPerformanceAmount(sum)
	if complete {
		out.Profit = protoPerformanceAmount(sum)
	} else {
		out.Reason = "contribution_history_incomplete"
	}
	for _, r := range h.Contributions {
		var share *big.Rat
		if complete && sum.Sign() != 0 && r.InScope && r.Profit != nil {
			share = contributionProfitShare(r.Profit, sum)
		}
		out.Rows = append(out.Rows, &pb.TrackerContribution{LedgerId: r.LedgerID, LedgerName: r.LedgerName, InstrumentKey: r.InstrumentKey, GroupId: r.GroupID, GroupName: r.GroupName, Reason: r.Reason, Opening: protoPerformanceAmount(r.Opening), Closing: protoPerformanceAmount(r.Closing), Flow: protoPerformanceAmount(r.Flow), Profit: protoPerformanceAmount(r.Profit), OpeningQuantity: protoPerformanceAmount(r.OpeningQuantity), ClosingQuantity: protoPerformanceAmount(r.ClosingQuantity), ProfitShare: protoPerformanceAmount(share), InScope: r.InScope})
	}
	return out, nil
}

// Shares are presentation percentages, unlike exact monetary contributions.
// Bounded twelve-decimal fractions avoid copying a long FX denominator into
// every row and withhold meaningless ratios near a zero net result.
func contributionProfitShare(profit, total *big.Rat) *big.Rat {
	if profit == nil || total == nil || total.Sign() == 0 {
		return nil
	}
	ratio := new(big.Rat).Quo(profit, total)
	if new(big.Rat).Abs(ratio).Cmp(big.NewRat(1000000000000, 1)) > 0 {
		return nil
	}
	result, _ := new(big.Rat).SetString(ratio.FloatString(12))
	return result
}
