package grpcadapter

import (
	"context"
	"math"
	"math/big"
	"slices"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *InvestmentNAVServer) GetTrackerPerformance(ctx context.Context, req *pb.GetTrackerPerformanceRequest) (*pb.GetInvestmentPerformanceResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	q, e := nav.NormalizeTrackerHistory(nav.TrackerHistoryRequest{LedgerIDs: req.GetLedgerIds(), Currency: req.GetCurrency(), FXPolicyKey: req.GetFxPolicyKey(), From: req.GetFromDate(), To: req.GetToDate(), PerformanceFrom: req.GetFromDate()}, time.Now().UTC())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	repo, ok := s.repo.(trackerHistoryRepo)
	if !ok || s.trackerSigner == nil {
		return nil, status.Error(codes.Unavailable, "portfolio performance unavailable")
	}
	if len(req.GetSnapshotTokens()) < 1 || len(req.GetSnapshotTokens()) > 119 {
		return nil, status.Error(codes.InvalidArgument, "portfolio snapshots required")
	}
	start, _ := time.Parse(time.DateOnly, q.From)
	cursor := start.AddDate(0, 0, -1).Format(time.DateOnly)
	days := []calc.PerformanceDay{}
	resets := map[string]bool{}
	basis, mode := "", ""
	version := int64(0)
	size := 0
	invalid := func() (*pb.GetInvestmentPerformanceResponse, error) {
		return nil, status.Error(codes.FailedPrecondition, "portfolio snapshot changed; refresh history")
	}
	for _, token := range req.GetSnapshotTokens() {
		size += len(token)
		if size > 3<<20 {
			return nil, status.Error(codes.InvalidArgument, "portfolio snapshot limit")
		}
		claim, e := s.trackerSigner.Open(token, time.Now().UTC())
		if e != nil {
			return invalid()
		}
		p := claim.Request
		if len(p.GroupIDs) > 0 || claim.Tenant != tenant || claim.Actor != actor || !slices.Equal(p.LedgerIDs, q.LedgerIDs) || p.Currency != q.Currency || p.FXPolicyKey != q.FXPolicyKey || p.To != q.To || p.PerformanceFrom != q.From {
			return invalid()
		}
		if basis == "" {
			basis, mode, version = claim.Basis, claim.FXMode, claim.FXVersion
		} else if basis != claim.Basis || version != claim.FXVersion || mode != claim.FXMode {
			return invalid()
		}
		for _, d := range claim.Days {
			if d.Date != cursor || d.Date > q.To || len(days) > 3661 {
				return invalid()
			}
			var value, flow *big.Rat
			if d.Value != "" {
				value, ok = new(big.Rat).SetString(d.Value)
				if !ok || value.Sign() < 0 {
					return invalid()
				}
			}
			if d.Flow != "" && d.FlowReason == "" {
				flow, ok = new(big.Rat).SetString(d.Flow)
				if !ok {
					return invalid()
				}
			}
			days = append(days, calc.PerformanceDay{Date: d.Date, NAV: value, ExternalFlow: flow, Complete: d.Complete})
			if d.Reset && d.Date >= q.From {
				resets[d.Date] = true
			}
			date, e := time.Parse(time.DateOnly, cursor)
			if e != nil {
				return invalid()
			}
			cursor = date.AddDate(0, 0, 1).Format(time.DateOnly)
		}
	}
	last, _ := time.Parse(time.DateOnly, q.To)
	if cursor != last.AddDate(0, 0, 1).Format(time.DateOnly) {
		return invalid()
	}
	performance, e := calc.CalculatePeriod(calc.PeriodPerformanceInput{Days: days, FlowCoverage: true, ResetDates: resets})
	if e != nil {
		return nil, status.Error(codes.Internal, "portfolio performance unavailable")
	}
	var benchmarks []nav.TrackerBenchmark
	if benchmarkRepo, ok := s.repo.(interface {
		TrackerBenchmarks(context.Context, string, string, nav.TrackerHistoryRequest, string) ([]nav.TrackerBenchmark, error)
	}); ok {
		baseline := days[0].Date
		for _, p := range performance.TWR.Points {
			if p.State == "baseline" {
				baseline = p.Date
				break
			}
		}
		benchmarks, e = benchmarkRepo.TrackerBenchmarks(ctx, tenant, actor, q, baseline)
		if e != nil {
			return nil, investmentRPCError(e)
		}
	}
	// Recheck current membership, selected ledger identities and scoped market
	// basis. A signed old value is never silently used after a correction.
	check := q
	check.From = q.To
	check.PerformanceFrom = ""
	fresh, e := repo.TrackerHistory(ctx, tenant, actor, check)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	if fresh.BasisKey != basis || fresh.FXPolicyVersion != version || fresh.FXMode != mode {
		return invalid()
	}
	// Percentages are presented to twelve decimals; exact monetary values and
	// all source daily factors remain independent of display precision.
	round := func(v *big.Rat) *big.Rat {
		if v == nil {
			return nil
		}
		f, _ := v.Float64()
		if math.IsInf(f, 0) || math.IsNaN(f) || math.Abs(f) > 1e12 {
			return nil
		}
		out, _ := new(big.Rat).SetString(v.FloatString(12))
		return out
	}
	performance.TWR.Return = round(performance.TWR.Return)
	if performance.TWR.Return == nil && performance.TWR.Reason == "" {
		performance.TWR.Reason = "numerical_range"
	}
	for i := range performance.TWR.Points {
		p := &performance.TWR.Points[i]
		p.DailyReturn = round(p.DailyReturn)
		p.SegmentReturn = round(p.SegmentReturn)
		if p.State == "calculated" && (p.DailyReturn == nil || p.SegmentReturn == nil) {
			p.State, p.Reason = "gap", "numerical_range"
		}
	}
	out := protoInvestmentPerformance(nav.Performance{PeriodPerformance: performance, Currency: q.Currency, FXPolicyKey: q.FXPolicyKey, Generation: fresh.Generation})
	analytics, e := calc.CalculateStrategyAnalytics(days, resets)
	if e != nil {
		return nil, status.Error(codes.Internal, "portfolio analytics unavailable")
	}
	out.AnalyticsConvention = "retrospective_cashless_strategy_v1"
	for _, p := range analytics.Points {
		out.AnalyticsPoints = append(out.AnalyticsPoints, &pb.TrackerAnalyticsPoint{Date: p.Date, Value: protoPerformanceAmount(p.Value), Flow: protoPerformanceAmount(p.Flow), Contributions: protoPerformanceAmount(p.Contributions), Invested: protoPerformanceAmount(p.Invested), Profit: protoPerformanceAmount(p.Profit), DailyProfit: protoPerformanceAmount(p.DailyProfit), Reason: p.Reason, FlowReason: p.FlowReason})
	}
	convertPeriods := func(periods []calc.StrategyAnalyticsPeriod) []*pb.TrackerAnalyticsPeriod {
		result := []*pb.TrackerAnalyticsPeriod{}
		for _, p := range periods {
			result = append(result, &pb.TrackerAnalyticsPeriod{Key: p.Key, FromDate: p.From, ToDate: p.To, Profit: protoPerformanceAmount(p.Profit), Reason: p.Reason})
		}
		return result
	}
	out.MonthlyProfit, out.YearlyProfit = convertPeriods(analytics.Months), convertPeriods(analytics.Years)
	analyticsByDate := map[string]calc.StrategyAnalyticsPoint{}
	for _, p := range analytics.Points {
		analyticsByDate[p.Date] = p
	}
	for _, b := range benchmarks {
		virtual := map[string]calc.BenchmarkPoint{}
		prices := map[string]*big.Rat{}
		for _, p := range b.Points {
			prices[p.Date] = p.Index
		}
		for i, d := range days {
			if d.Date != b.BaseDate {
				continue
			}
			if d.Complete && d.NAV != nil {
				input := []calc.BenchmarkDay{}
				for j := i; j < len(days); j++ {
					price := prices[days[j].Date]
					input = append(input, calc.BenchmarkDay{Date: days[j].Date, Price: price, ExternalFlow: days[j].ExternalFlow, HistoryReset: resets[days[j].Date]})
				}
				result, err := calc.VirtualStrategyValues(d.NAV, input)
				if err != nil {
					return nil, status.Error(codes.Internal, "benchmark simulation unavailable")
				}
				for _, p := range result.Points {
					virtual[p.Date] = p
				}
			}
			break
		}
		line := &pb.TrackerBenchmarkLine{Key: b.Key, Title: b.Title, Kind: b.Kind, NativeCurrency: b.NativeCurrency, Provider: b.Provider, SourceUrl: b.SourceURL, BaseDate: b.BaseDate, Reason: b.Reason, ReadAt: optionalInvestmentTimestamp(b.ReadAt)}
		for _, p := range b.Points {
			v, found := virtual[p.Date]
			if !found {
				v.Reason = "benchmark_baseline_unavailable"
			}
			var virtualProfit *big.Rat
			if v.Value != nil && analyticsByDate[p.Date].Invested != nil {
				virtualProfit = new(big.Rat).Sub(v.Value, analyticsByDate[p.Date].Invested)
			}
			line.Points = append(line.Points, &pb.TrackerBenchmarkPoint{Date: p.Date, Index: protoPerformanceAmount(p.Index), Reason: p.Reason, VirtualValue: protoPerformanceAmount(v.Value), VirtualReason: v.Reason, VirtualProfit: protoPerformanceAmount(virtualProfit)})
		}
		out.TrackerBenchmarks = append(out.TrackerBenchmarks, line)
	}
	out.HistoryScope = "recorded_securities_strategy"
	out.TwrConvention = "securities_daily_end_of_day_flows"
	return out, nil
}
