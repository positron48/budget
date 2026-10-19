package grpcadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"math/big"
	"slices"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type trackerChartRepo interface {
	trackerHistoryRepo
	TrackerBenchmarks(context.Context, string, string, nav.TrackerHistoryRequest, string) ([]nav.TrackerBenchmark, error)
	ReadTrackerChart(context.Context, string, string, string, string) ([]byte, error)
	SaveTrackerChart(context.Context, string, string, string, string, []byte, []byte, bool) error
	TrackerChartsToRefresh(context.Context) ([]nav.TrackerChartRefresh, error)
}

// A single cold calculation per server bounds memory and avoids duplicate
// foreground/background work. Warm reads never wait behind a cold calculation.
var trackerChartCalculation = make(chan struct{}, 1)

func chartHash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func (s *InvestmentNAVServer) GetTrackerChart(ctx context.Context, req *pb.GetTrackerChartRequest) (*pb.GetTrackerChartResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return s.trackerChart(ctx, req, false)
}

func (s *InvestmentNAVServer) trackerChart(ctx context.Context, req *pb.GetTrackerChartRequest, force bool) (*pb.GetTrackerChartResponse, error) {
	tenant, actor, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	q, err := nav.NormalizeTrackerHistory(nav.TrackerHistoryRequest{LedgerIDs: req.GetLedgerIds(), GroupIDs: req.GetGroupIds(), InstrumentKeys: req.GetInstrumentKeys(), Currency: req.GetCurrency(), FXPolicyKey: req.GetFxPolicyKey(), From: req.GetFromDate(), To: req.GetToDate()}, time.Now().UTC())
	if err != nil {
		return nil, investmentRPCError(err)
	}
	repo, ok := s.repo.(trackerChartRepo)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "chart projection unavailable")
	}
	req = &pb.GetTrackerChartRequest{LedgerIds: q.LedgerIDs, GroupIds: q.GroupIDs, InstrumentKeys: q.InstrumentKeys, Currency: q.Currency, FxPolicyKey: q.FXPolicyKey, FromDate: q.From, ToDate: q.To}
	request, _ := proto.MarshalOptions{Deterministic: true}.Marshal(req)
	scope := chartHash(append([]byte("tracker-chart-v8:"), request...))
	fence := func() (string, error) {
		check := q
		check.BasisOnly = true
		check.From = q.To
		check.CompactIssues = true
		h, e := repo.TrackerHistory(ctx, tenant, actor, check)
		if e != nil {
			return "", investmentRPCError(e)
		}
		// Built-in benchmarks have their own provider facts/entitlements and may not
		// be personal holdings. Fence them as well as the portfolio's scoped basis.
		b, e := repo.TrackerBenchmarks(ctx, tenant, actor, q, q.From)
		if e != nil {
			return "", investmentRPCError(e)
		}
		for i := range b {
			b[i].ReadAt = time.Time{}
		}
		raw, e := json.Marshal(struct {
			Basis      string
			Benchmarks []nav.TrackerBenchmark
		}{h.BasisKey, b})
		return chartHash(raw), e
	}
	basis, err := fence()
	if err != nil {
		return nil, err
	}
	read := func() (*pb.GetTrackerChartResponse, error) {
		raw, e := repo.ReadTrackerChart(ctx, tenant, actor, scope, basis)
		if e != nil {
			return nil, investmentRPCError(e)
		}
		if len(raw) == 0 {
			return nil, nil
		}
		out := &pb.GetTrackerChartResponse{}
		if proto.Unmarshal(raw, out) != nil {
			return nil, nil
		}
		out.CacheHit = true
		return out, nil
	}
	if !force {
		if hit, e := read(); hit != nil || e != nil {
			return hit, e
		}
	}
	select {
	case trackerChartCalculation <- struct{}{}:
		defer func() { <-trackerChartCalculation }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	// Reauthorize after waiting; sources or membership may have changed.
	basis, err = fence()
	if err != nil {
		return nil, err
	}
	// A different request may have populated this view while we waited.
	if !force {
		if hit, e := read(); hit != nil || e != nil {
			return hit, e
		}
	}
	calculate := func(keys []string) (*pb.TrackerChartCalculation, error) {
		out := &pb.TrackerChartCalculation{}
		tokens := []string{}
		cursor := q.From
		for n := 0; cursor != "" && n < 119; n++ {
			page, e := s.GetTrackerHistory(ctx, &pb.GetTrackerHistoryRequest{LedgerIds: q.LedgerIDs, GroupIds: q.GroupIDs, InstrumentKeys: keys, Currency: q.Currency, FxPolicyKey: q.FXPolicyKey, FromDate: cursor, ToDate: q.To, PerformanceFromDate: q.From, MonetaryOnly: len(q.GroupIDs) > 0, CompactIssues: true, PageDays: 93})
			if e != nil {
				return nil, e
			}
			if len(out.Pages) > 0 && page.BasisKey != out.Pages[0].BasisKey {
				return nil, status.Error(codes.FailedPrecondition, "portfolio history changed")
			}
			tokens = append(tokens, page.PerformanceSnapshot)
			page.PerformanceSnapshot = ""
			out.Pages = append(out.Pages, page)
			cursor = page.NextDate
		}
		if cursor != "" {
			return nil, status.Error(codes.ResourceExhausted, "chart range limit")
		}
		out.Performance, err = s.GetTrackerPerformance(ctx, &pb.GetTrackerPerformanceRequest{LedgerIds: q.LedgerIDs, GroupIds: q.GroupIDs, InstrumentKeys: keys, Currency: q.Currency, FxPolicyKey: q.FXPolicyKey, FromDate: q.From, ToDate: q.To, MonetaryOnly: len(q.GroupIDs) > 0, SnapshotTokens: tokens})
		return out, err
	}
	out := &pb.GetTrackerChartResponse{}
	out.Portfolio, err = calculate(q.InstrumentKeys)
	if err != nil {
		return nil, err
	}
	excluded := map[string]bool{}
	for _, p := range out.Portfolio.Pages {
		for _, k := range p.MonetaryExcludedKeys {
			excluded[k] = true
		}
	}
	for k := range excluded {
		out.ExcludedKeys = append(out.ExcludedKeys, k)
	}
	slices.Sort(out.ExcludedKeys)
	if len(excluded) > 0 {
		keys := []string{}
		for _, k := range out.Portfolio.Pages[0].AnalyticsInstrumentKeys {
			if !excluded[k] {
				keys = append(keys, k)
			}
		}
		if len(keys) > 0 {
			out.KnownPortfolio, err = calculate(keys)
			if err != nil {
				return nil, err
			}
		}
	}
	// TWR needs every intermediate valuation. Keep its stricter fixed scope
	// distinct from monetary profit, which can recover across temporary gaps.
	strict := map[string]bool{}
	for _, page := range out.Portfolio.Pages {
		for _, k := range page.AnalyticsExcludedKeys {
			strict[k] = true
		}
	}
	if len(strict) > len(excluded) {
		keys := []string{}
		for _, k := range out.Portfolio.Pages[0].AnalyticsInstrumentKeys {
			if !strict[k] {
				keys = append(keys, k)
			}
		}
		if len(keys) > 0 {
			out.GrowthPortfolio, err = calculate(keys)
			if err != nil {
				return nil, err
			}
		}
	}
	monetary := out.Portfolio.Performance
	if out.KnownPortfolio != nil {
		monetary = out.KnownPortfolio.Performance
	}
	if err = s.fillKnownTrackerPeriods(ctx, q, monetary.InstrumentKeys, monetary); err != nil {
		return nil, err
	}
	fresh, err := fence()
	if err != nil {
		return nil, err
	}
	if fresh != basis {
		return nil, status.Error(codes.FailedPrecondition, "portfolio history changed")
	}
	out.ComputedAt = timestamppb.Now()
	raw, err := proto.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(raw) > 16<<20 {
		return nil, status.Error(codes.ResourceExhausted, "chart projection limit")
	}
	if err = repo.SaveTrackerChart(ctx, tenant, actor, scope, basis, request, raw, !force); err != nil {
		return nil, investmentRPCError(err)
	}
	return out, nil
}

// A missing boundary price must not turn the whole month into zero. Calculate
// the known contribution on a fixed per-period security scope, excluding a
// security on every account if any of its boundary values or flows are unknown.
// Keep Profit absent: known totals across changing scopes need not add up to
// the full range's profit, and the UI/export must identify them as partial.
func (s *InvestmentNAVServer) fillKnownTrackerPeriods(ctx context.Context, q nav.TrackerHistoryRequest, keys []string, p *pb.GetInvestmentPerformanceResponse) error {
	for _, periods := range [][]*pb.TrackerAnalyticsPeriod{p.MonthlyProfit, p.YearlyProfit} {
		for _, period := range periods {
			if period.Profit != nil || period.Reason != "valuation_missing" {
				continue
			}
			blocked := false
			for _, day := range p.AnalyticsPoints {
				if day.Date >= period.FromDate && day.Date <= period.ToDate && (day.Flow == nil || day.FlowReason == "history_reset") {
					blocked = true
					break
				}
			}
			if blocked {
				continue
			}
			part, err := s.GetTrackerContributions(ctx, &pb.GetTrackerContributionsRequest{LedgerIds: q.LedgerIDs, GroupIds: q.GroupIDs, InstrumentKeys: keys, Currency: q.Currency, FxPolicyKey: q.FXPolicyKey, FromDate: period.FromDate, ToDate: period.ToDate})
			if err != nil {
				return err
			}
			bad := map[string]bool{}
			for _, row := range part.Rows {
				if row.InScope && row.Profit == nil {
					bad[row.InstrumentKey] = true
				}
			}
			sum, count := new(big.Rat), 0
			for _, row := range part.Rows {
				if row.InScope && !bad[row.InstrumentKey] && row.Profit != nil {
					amount, ok := new(big.Rat).SetString(row.Profit.Numerator + "/" + row.Profit.Denominator)
					if !ok {
						return status.Error(codes.Internal, "period contribution unavailable")
					}
					sum.Add(sum, amount)
					count++
				}
			}
			if count > 0 && len(bad) > 0 && !bad[""] {
				period.KnownProfit = protoPerformanceAmount(sum)
				for key := range bad {
					period.ExcludedInstrumentKeys = append(period.ExcludedInstrumentKeys, key)
				}
				slices.Sort(period.ExcludedInstrumentKeys)
			}
		}
	}
	return nil
}

// Recent requested scopes are refreshed every six hours; reads always verify
// the current source basis, including between periodic refreshes.
func (s *InvestmentNAVServer) RunTrackerChartRefresh(ctx context.Context) {
	repo, ok := s.repo.(trackerChartRepo)
	if !ok {
		return
	}
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rows, err := repo.TrackerChartsToRefresh(ctx)
			if err != nil {
				continue
			}
			for _, v := range rows {
				req := &pb.GetTrackerChartRequest{}
				if proto.Unmarshal(v.Request, req) != nil {
					continue
				}
				work, cancel := context.WithTimeout(ctx, 5*time.Minute)
				work = ctxutil.WithUserID(ctxutil.WithTenantID(work, v.Tenant), v.Actor)
				_, err = s.trackerChart(work, req, true)
				if err != nil {
					log.Print("Tracker chart background refresh failed")
				}
				// A recent view ending yesterday rolls forward before the next visit.
				// Explicit historical end dates remain unchanged.
				loc, _ := time.LoadLocation("Europe/Moscow")
				oldLast := v.ComputedAt.In(loc).AddDate(0, 0, -1).Format(time.DateOnly)
				newLast := time.Now().In(loc).AddDate(0, 0, -1).Format(time.DateOnly)
				if err == nil && req.ToDate == oldLast && newLast > oldLast {
					req.ToDate = newLast
					_, err = s.trackerChart(work, req, false)
					if err != nil {
						log.Print("Tracker chart daily extension failed")
					}
				}
				cancel()
				if ctx.Err() != nil {
					return
				}
			}
		}
	}
}
