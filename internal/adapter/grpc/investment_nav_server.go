package grpcadapter

import (
	"context"
	"math"
	"math/big"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

type InvestmentNAVRepo interface {
	CurrentNAVForSeries(context.Context, string, string, string) (nav.CurrentNAV, error)
	NAVVariant(context.Context, string, string, string, string, string) (nav.Variant, error)
	PrepareNAVVariant(context.Context, string, string, string, string, string) (nav.Variant, error)
	ExportSeries(context.Context, string, string, string, string, string) (nav.SeriesCSV, error)
	OverviewForSeries(context.Context, string, string, string, string, string) (nav.Overview, error)
	ProfitForSeries(context.Context, string, string, string, string, string) (nav.ProfitAnalytics, error)
	InvestmentInstrumentPrices(context.Context, string, string, string, string, string, string, []string) (nav.InstrumentPriceHistory, error)
	InvestmentPositions(context.Context, string, string, string, string, []string) (nav.PositionDetails, error)
	InvestmentInstrument(context.Context, string, string, string, string, string, string, []string) (nav.InstrumentDetails, error)
	InvestmentCompositionHistory(context.Context, string, string, string, string, string, []string, string) (nav.CompositionHistory, error)
	GetInstrumentGroups(context.Context, string, string) (nav.InstrumentGroups, []nav.GroupPosition, error)
	SaveInstrumentGroups(context.Context, string, string, nav.SaveInstrumentGroups) (nav.InstrumentGroups, error)
	ListBenchmarks(context.Context, string, string, string) ([]nav.SavedBenchmarkProfile, error)
	BenchmarkForSeries(context.Context, string, string, string, string, string, string) (nav.BenchmarkComparison, error)
	PerformanceForSeries(context.Context, string, string, string, string, string) (nav.Performance, error)
	GetNAVSeries(context.Context, string, string, string) (nav.SeriesSummary, error)
	SaveInvestmentPortfolio(context.Context, string, string, nav.PortfolioInput) (nav.Portfolio, error)
	ListInvestmentPortfolios(context.Context, string, string, string) ([]nav.Portfolio, error)
	InvestmentPortfolioOptions(context.Context, string, string, string, string) (nav.PortfolioOptions, error)
	ListNAVSeries(context.Context, string, string, string) ([]nav.SeriesSummary, error)
	NAVHistoryForSeries(context.Context, string, string, string, string, string) ([]nav.DailyNAV, error)
	RequestNAVRefresh(context.Context, string, string, string) error
}

type InvestmentNAVServer struct {
	pb.UnimplementedInvestmentNAVServiceServer
	repo          InvestmentNAVRepo
	trackerSigner *nav.TrackerSnapshotSigner
}

func NewInvestmentNAVServer(repo InvestmentNAVRepo, snapshotKey ...string) *InvestmentNAVServer {
	s := &InvestmentNAVServer{repo: repo}
	if len(snapshotKey) > 0 {
		s.trackerSigner = nav.NewTrackerSnapshotSigner(snapshotKey[0])
	}
	return s
}
func (s *InvestmentNAVServer) ListInvestmentNAVSeries(ctx context.Context, req *pb.ListInvestmentNAVSeriesRequest) (*pb.ListInvestmentNAVSeriesResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	series, e := s.repo.ListNAVSeries(ctx, tenant, actor, req.GetAfterKey())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.ListInvestmentNAVSeriesResponse{}
	for _, v := range series {
		out.Series = append(out.Series, &pb.InvestmentNAVSeries{ExtensionLimited: v.ExtensionLimited, Key: v.Key, LedgerIds: v.LedgerIDs, Currency: v.Currency, FxPolicyKey: v.FXPolicyKey, FromDate: v.From, ToDate: v.To})
	}
	if len(series) == 100 {
		out.NextAfterKey = series[len(series)-1].Key
	}
	return out, nil
}
func (s *InvestmentNAVServer) GetInvestmentNAVHistory(ctx context.Context, req *pb.GetInvestmentNAVHistoryRequest) (*pb.GetInvestmentNAVHistoryResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	points, e := s.repo.NAVHistoryForSeries(ctx, tenant, actor, req.GetSeriesKey(), req.GetFromDate(), req.GetToDate())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentNAVHistoryResponse{ReportingTimezone: "Europe/Moscow"}
	for _, v := range points {
		out.Points = append(out.Points, protoNAVPoint(v))
	}
	return out, nil
}
func protoNAVPoint(v nav.DailyNAV) *pb.InvestmentNAVPoint {
	out := &pb.InvestmentNAVPoint{Date: v.Date, State: v.State, Generation: v.Generation, ComputedAt: optionalInvestmentTimestamp(v.ComputedAt)}
	// Defense in depth: even an accidental stale Result from storage cannot leak
	// a superseded amount to a client that might plot it as a current valuation.
	if (v.State != "ready" && v.State != "incomplete") || v.Result == nil {
		return out
	}
	r := v.Result
	out.Currency = r.Currency
	out.FxPolicy = r.FXPolicy
	out.AsOf = optionalInvestmentTimestamp(r.At)
	if r.KnownValue != nil {
		out.KnownValue = &pb.InvestmentNAVAmount{Numerator: r.KnownValue.Num().String(), Denominator: r.KnownValue.Denom().String(), Decimal: r.KnownValue.FloatString(12)}
	}
	for _, i := range r.Issues {
		out.Issues = append(out.Issues, &pb.InvestmentNAVIssue{ComponentId: i.ComponentID, Code: i.Code})
	}
	for _, i := range r.Warnings {
		out.Warnings = append(out.Warnings, &pb.InvestmentNAVIssue{ComponentId: i.ComponentID, Code: i.Code})
	}
	return out
}
func (s *InvestmentNAVServer) RequestInvestmentNAVRefresh(ctx context.Context, req *pb.RequestInvestmentNAVRefreshRequest) (*pb.RequestInvestmentNAVRefreshResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	if e = s.repo.RequestNAVRefresh(ctx, tenant, actor, req.GetSeriesKey()); e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.RequestInvestmentNAVRefreshResponse{State: "requested"}, nil
}

func protoInvestmentPortfolio(p nav.Portfolio) *pb.InvestmentPortfolio {
	return &pb.InvestmentPortfolio{AutoExtend: p.AutoExtend, Archived: p.Archived, Id: p.ID, Name: p.Name, LedgerIds: p.LedgerIDs, Currency: p.Currency, FxPolicyKey: p.FXPolicyKey, ConventionKey: p.ConventionKey, FromDate: p.From, ToDate: p.To, SeriesKey: p.SeriesKey, Version: p.Version, UpdatedAt: optionalInvestmentTimestamp(p.UpdatedAt)}
}
func (s *InvestmentNAVServer) SaveInvestmentPortfolio(ctx context.Context, req *pb.SaveInvestmentPortfolioRequest) (*pb.SaveInvestmentPortfolioResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	p, e := s.repo.SaveInvestmentPortfolio(ctx, tenant, actor, nav.PortfolioInput{AutoExtend: req.GetAutoExtend(), Archived: req.GetArchived(), ID: req.GetId(), Name: req.GetName(), LedgerIDs: req.GetLedgerIds(), Currency: req.GetCurrency(), FXPolicyKey: req.GetFxPolicyKey(), ConventionKey: req.GetConventionKey(), From: req.GetFromDate(), To: req.GetToDate(), ExpectedVersion: req.GetExpectedVersion(), RequestKey: req.GetRequestKey()})
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.SaveInvestmentPortfolioResponse{Portfolio: protoInvestmentPortfolio(p)}, nil
}
func (s *InvestmentNAVServer) ListInvestmentPortfolios(ctx context.Context, req *pb.ListInvestmentPortfoliosRequest) (*pb.ListInvestmentPortfoliosResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	list, e := s.repo.ListInvestmentPortfolios(ctx, tenant, actor, req.GetAfterId())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.ListInvestmentPortfoliosResponse{}
	for _, p := range list {
		out.Portfolios = append(out.Portfolios, protoInvestmentPortfolio(p))
	}
	if len(list) == 100 {
		out.NextAfterId = list[len(list)-1].ID
	}
	return out, nil
}
func (s *InvestmentNAVServer) ListInvestmentValuationProfiles(ctx context.Context, req *pb.ListInvestmentValuationProfilesRequest) (*pb.ListInvestmentValuationProfilesResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	options, e := s.repo.InvestmentPortfolioOptions(ctx, tenant, actor, req.GetAfterPriceKey(), req.GetAfterFxKey())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.ListInvestmentValuationProfilesResponse{}
	for _, v := range options.Price {
		out.PriceProfiles = append(out.PriceProfiles, &pb.InvestmentValuationProfileOption{Key: v.Key, Title: v.Title, Mode: v.Mode, Version: v.Version})
	}
	for _, v := range options.FX {
		out.FxProfiles = append(out.FxProfiles, &pb.InvestmentValuationProfileOption{Key: v.Key, Title: v.Title, Mode: v.Mode, Version: v.Version})
	}
	if len(options.Price) == 100 {
		out.NextPriceKey = options.Price[99].Key
	}
	if len(options.FX) == 100 {
		out.NextFxKey = options.FX[99].Key
	}
	return out, nil
}

func (s *InvestmentNAVServer) GetInvestmentNAVSeries(ctx context.Context, req *pb.GetInvestmentNAVSeriesRequest) (*pb.GetInvestmentNAVSeriesResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.repo.GetNAVSeries(ctx, tenant, actor, req.GetSeriesKey())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.GetInvestmentNAVSeriesResponse{Series: &pb.InvestmentNAVSeries{Key: v.Key, LedgerIds: v.LedgerIDs, Currency: v.Currency, FxPolicyKey: v.FXPolicyKey, FromDate: v.From, ToDate: v.To, ExtensionLimited: v.ExtensionLimited}}, nil
}

func protoPerformanceAmount(value *big.Rat) *pb.InvestmentNAVAmount {
	if value == nil {
		return nil
	}
	return &pb.InvestmentNAVAmount{Numerator: value.Num().String(), Denominator: value.Denom().String(), Decimal: value.FloatString(12)}
}
func (s *InvestmentNAVServer) GetInvestmentPerformance(ctx context.Context, req *pb.GetInvestmentPerformanceRequest) (*pb.GetInvestmentPerformanceResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	result, e := s.repo.PerformanceForSeries(ctx, tenant, actor, req.GetSeriesKey(), req.GetFromDate(), req.GetToDate())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return protoInvestmentPerformance(result), nil
}
func protoInvestmentPerformance(result nav.Performance) *pb.GetInvestmentPerformanceResponse {
	out := &pb.GetInvestmentPerformanceResponse{FromDate: result.From, ToDate: result.To, Currency: result.Currency, FxPolicyKey: result.FXPolicyKey, Generation: result.Generation, Reason: result.Reason, NetContributionsReason: result.FlowReason, TwrReason: result.TWR.Reason, XirrReason: result.XIRR.Reason, XirrDays: int32(result.XIRR.Days), XirrAnnualized: true, TwrConvention: "daily_end_of_day_flows", ReportingTimezone: "Europe/Moscow", HistoryScope: "recorded_journal"}
	if result.Reason == "" {
		out.Profit = protoPerformanceAmount(result.Profit)
	}
	if result.FlowReason == "" {
		out.NetContributions = protoPerformanceAmount(result.NetContributions)
	}
	if result.TWR.Reason == "" {
		out.Twr = protoPerformanceAmount(result.TWR.Return)
	}
	if result.XIRR.Reason == "" && result.XIRR.Rate != nil {
		rate := *result.XIRR.Rate
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= -1 {
			out.XirrReason = "numerical_range"
		} else {
			out.Xirr = &rate
		}
	}
	for _, point := range result.TWR.Points {
		p := &pb.InvestmentTWRPoint{Date: point.Date, State: point.State, Reason: point.Reason, Segment: int32(point.Segment), Index: protoPerformanceAmount(calc.TWRIndex(point))}
		if point.State == "calculated" {
			p.DailyReturn = protoPerformanceAmount(point.DailyReturn)
			p.SegmentReturn = protoPerformanceAmount(point.SegmentReturn)
		}
		out.TwrPoints = append(out.TwrPoints, p)
	}
	return out
}

func protoBenchmark(p nav.SavedBenchmarkProfile) *pb.InvestmentBenchmark {
	return &pb.InvestmentBenchmark{Key: p.Key, Title: p.Title, Kind: p.Kind, NativeCurrency: p.Currency, Version: p.Version}
}
func (s *InvestmentNAVServer) ListInvestmentBenchmarks(ctx context.Context, req *pb.ListInvestmentBenchmarksRequest) (*pb.ListInvestmentBenchmarksResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	profiles, e := s.repo.ListBenchmarks(ctx, tenant, actor, req.GetAfterKey())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.ListInvestmentBenchmarksResponse{}
	for _, profile := range profiles {
		out.Benchmarks = append(out.Benchmarks, protoBenchmark(profile))
	}
	if len(profiles) == 100 {
		out.NextAfterKey = profiles[99].Key
	}
	return out, nil
}
func (s *InvestmentNAVServer) GetInvestmentBenchmarkComparison(ctx context.Context, req *pb.GetInvestmentBenchmarkComparisonRequest) (*pb.GetInvestmentBenchmarkComparisonResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	result, e := s.repo.BenchmarkForSeries(ctx, tenant, actor, req.GetSeriesKey(), req.GetBenchmarkKey(), req.GetFromDate(), req.GetToDate())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentBenchmarkComparisonResponse{Benchmark: protoBenchmark(result.Profile), Currency: result.Portfolio.Currency, FxPolicyKey: result.Portfolio.FXPolicyKey, Generation: result.Portfolio.Generation, PriceProfileVersion: result.PriceProfileVersion, Reason: result.Result.Reason, Method: "fractional_units_external_flows", ReportingTimezone: "Europe/Moscow"}
	values := map[string]*big.Rat{}
	for _, day := range result.Portfolio.Days {
		if day.Complete {
			values[day.Date] = day.NAV
		}
	}
	normalized := map[string]*big.Rat{}
	if len(result.Portfolio.Days) > 0 && result.Portfolio.Days[0].Complete && result.Portfolio.Days[0].NAV != nil && result.Portfolio.Days[0].NAV.Sign() > 0 {
		for _, point := range result.Portfolio.TWR.Points {
			if point.Segment != 1 {
				continue
			}
			normalized[point.Date] = calc.TWRIndex(point)
		}
	}
	evidence := map[string]nav.BenchmarkDailyProvenance{}
	for _, point := range result.Evidence {
		evidence[point.Date] = point
	}
	for _, point := range result.Result.Points {
		p := &pb.InvestmentBenchmarkPoint{Date: point.Date, State: point.State, Reason: point.Reason, Index: protoPerformanceAmount(point.Index), PortfolioIndex: protoPerformanceAmount(normalized[point.Date]), PortfolioValue: protoPerformanceAmount(values[point.Date])}
		if point.State == "ready" {
			p.Value = protoPerformanceAmount(point.Value)
		}
		if point.State == "ready" || point.State == "gap" || point.State == "pending" {
			p.Units = protoPerformanceAmount(point.Units)
			p.Cash = protoPerformanceAmount(point.Cash)
			p.PendingWithdrawal = protoPerformanceAmount(point.PendingWithdrawal)
		}
		if ev, ok := evidence[point.Date]; ok {
			p.Issues = ev.Issues
			p.ObservationId = ev.ObservationID
			p.ObservationRevision = ev.ObservationRevision
			p.CalendarId = ev.CalendarID
			p.CalendarRevision = ev.CalendarRevision
		}
		out.Points = append(out.Points, p)
	}
	return out, nil
}
