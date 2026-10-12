package grpcadapter

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type navRepoStub struct {
	InvestmentNAVRepo
	tenant, actor string
	points        []nav.DailyNAV
	err           error
}

func (r *navRepoStub) NAVHistoryForSeries(_ context.Context, tenant, actor, key, from, to string) ([]nav.DailyNAV, error) {
	r.tenant = tenant
	r.actor = actor
	return r.points, r.err
}

func TestNAVRPCPrecisionGapsAndIdentity(t *testing.T) {
	value := &nav.Result{Currency: "USD", FXPolicy: "market@2", PortfolioValue: calc.PortfolioValue{Valuation: calc.Valuation{KnownValue: big.NewRat(1, 3), Issues: []calc.QualityIssue{{ComponentID: "bond", Code: "accrued_missing"}}}}}
	repo := &navRepoStub{points: []nav.DailyNAV{{Date: "2024-01-01", State: "incomplete", Result: value}, {Date: "2024-01-02", State: "stale", Result: value}, {Date: "2024-01-03", State: "unavailable"}}}
	server := NewInvestmentNAVServer(repo)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	result, e := server.GetInvestmentNAVHistory(ctx, &pb.GetInvestmentNAVHistoryRequest{SeriesKey: "key", FromDate: "2024-01-01", ToDate: "2024-01-03"})
	if e != nil || repo.tenant != "tenant" || repo.actor != "actor" || result.ReportingTimezone != "Europe/Moscow" {
		t.Fatal(result, e)
	}
	p := result.Points[0]
	if p.State != "incomplete" || p.KnownValue.Numerator != "1" || p.KnownValue.Denominator != "3" || p.KnownValue.Decimal != "0.333333333333" || len(p.Issues) != 1 {
		t.Fatal(p)
	}
	for _, p := range result.Points[1:] {
		if p.KnownValue != nil || len(p.Issues) != 0 || p.Currency != "" {
			t.Fatal("stale amount disclosed", p)
		}
	}
	raw, e := protojson.Marshal(result)
	if e != nil || !strings.Contains(string(raw), `"numerator":"1"`) {
		t.Fatal(string(raw), e)
	}
	if _, e = server.GetInvestmentNAVHistory(context.Background(), nil); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	repo.err = errors.New("private database credentials")
	if _, e = server.GetInvestmentNAVHistory(ctx, nil); status.Code(e) != codes.Internal || strings.Contains(e.Error(), "credentials") {
		t.Fatal(e)
	}
}

type performanceRepoStub struct {
	InvestmentNAVRepo
	result        nav.Performance
	tenant, actor string
	err           error
}

func (r *performanceRepoStub) PerformanceForSeries(_ context.Context, tenant, actor, key, from, to string) (nav.Performance, error) {
	r.tenant = tenant
	r.actor = actor
	return r.result, r.err
}
func TestPerformanceRPCPresencePrecisionAndIdentity(t *testing.T) {
	rate := 0.0
	repo := &performanceRepoStub{result: nav.Performance{PeriodPerformance: calc.PeriodPerformance{Profit: big.NewRat(1, 3), TWR: calc.TWRResult{Return: big.NewRat(1, 50)}, XIRR: calc.XIRRResult{Rate: &rate, Annualized: true}}}}
	server := NewInvestmentNAVServer(repo)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	result, err := server.GetInvestmentPerformance(ctx, nil)
	if err != nil || repo.tenant != "tenant" || repo.actor != "actor" || result.Profit.Numerator != "1" || result.Profit.Denominator != "3" || result.Xirr == nil || *result.Xirr != 0 || result.Twr.Decimal != "0.020000000000" || result.HistoryScope != "recorded_journal" {
		t.Fatal(result, err)
	}
	repo.result.Reason = "initial_nav_unknown"
	repo.result.TWR.Reason = "discontinuous_history"
	repo.result.XIRR.Reason = "no_root"
	result, err = server.GetInvestmentPerformance(ctx, nil)
	if err != nil || result.Profit != nil || result.Twr != nil || result.Xirr != nil {
		t.Fatal("undefined exposed", result, err)
	}
	if _, err = server.GetInvestmentPerformance(context.Background(), nil); status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	repo.err = errors.New("private financial data")
	if _, err = server.GetInvestmentPerformance(ctx, nil); status.Code(err) != codes.Internal || strings.Contains(err.Error(), "financial") {
		t.Fatal(err)
	}
}

type benchmarkRepoStub struct {
	InvestmentNAVRepo
	result        nav.BenchmarkComparison
	tenant, actor string
}

func (r *benchmarkRepoStub) BenchmarkForSeries(_ context.Context, tenant, actor, series, key, from, to string) (nav.BenchmarkComparison, error) {
	r.tenant = tenant
	r.actor = actor
	return r.result, nil
}
func TestBenchmarkRPCDoesNotExposeIncompleteMoney(t *testing.T) {
	repo := &benchmarkRepoStub{result: nav.BenchmarkComparison{Portfolio: nav.Performance{Currency: "RUB", Days: []calc.PerformanceDay{{Date: "2026-01-01", NAV: big.NewRat(50, 1), Complete: false}}}, Result: calc.BenchmarkResult{Points: []calc.BenchmarkPoint{{Date: "2026-01-01", State: "pending", Value: big.NewRat(999, 1), Index: big.NewRat(120, 1), Cash: big.NewRat(10, 1), PendingWithdrawal: big.NewRat(20, 1)}}}}}
	server := NewInvestmentNAVServer(repo)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	out, e := server.GetInvestmentBenchmarkComparison(ctx, nil)
	if e != nil || repo.tenant != "tenant" || repo.actor != "actor" || out.Points[0].Value != nil || out.Points[0].PortfolioValue != nil || out.Points[0].Index.Numerator != "120" || out.Points[0].Cash.Numerator != "10" {
		t.Fatal(out, e)
	}
	if _, e = server.GetInvestmentBenchmarkComparison(context.Background(), nil); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
}

func TestBenchmarkRPCDoesNotJoinRebasedPortfolioReturns(t *testing.T) {
	repo := &benchmarkRepoStub{result: nav.BenchmarkComparison{
		Portfolio: nav.Performance{Days: []calc.PerformanceDay{{Date: "2026-01-01", NAV: big.NewRat(100, 1), Complete: true}}, PeriodPerformance: calc.PeriodPerformance{TWR: calc.TWRResult{Points: []calc.TWRPoint{{Date: "2026-01-01", State: "baseline", Segment: 1}, {Date: "2026-01-02", State: "calculated", Segment: 2, SegmentReturn: big.NewRat(1, 5)}}}}},
		Result:    calc.BenchmarkResult{Points: []calc.BenchmarkPoint{{Date: "2026-01-01", State: "ready"}, {Date: "2026-01-02", State: "ready"}}},
	}}
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	out, e := NewInvestmentNAVServer(repo).GetInvestmentBenchmarkComparison(ctx, nil)
	if e != nil || out.Points[0].PortfolioIndex == nil || out.Points[0].PortfolioIndex.Numerator != "100" || out.Points[1].PortfolioIndex != nil {
		t.Fatal(out, e)
	}
}

func TestPerformanceRPCBase100IndexPrecisionAndGaps(t *testing.T) {
	p := nav.Performance{PeriodPerformance: calc.PeriodPerformance{TWR: calc.TWRResult{Points: []calc.TWRPoint{
		{Date: "2024-01-01", State: "baseline", Segment: 1},
		{Date: "2024-01-02", State: "calculated", Segment: 1, SegmentReturn: big.NewRat(1, 3)},
		{Date: "2024-01-03", State: "gap", Segment: 1, SegmentReturn: big.NewRat(999, 1)},
		{Date: "2024-01-04", State: "baseline", Segment: 2},
	}}}}
	out := protoInvestmentPerformance(p)
	if out.TwrPoints[0].Index.Numerator != "100" || out.TwrPoints[1].Index.Numerator != "400" || out.TwrPoints[1].Index.Denominator != "3" || out.TwrPoints[2].Index != nil || out.TwrPoints[3].Index.Numerator != "100" {
		t.Fatal(out)
	}
}
