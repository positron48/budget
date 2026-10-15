package grpcadapter

import (
	"context"
	"math/big"
	"strings"
	"testing"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type trackerPerformanceStub struct {
	InvestmentNAVRepo
	basis string
}

func (r *trackerPerformanceStub) TrackerHistory(_ context.Context, _ string, _ string, q nav.TrackerHistoryRequest) (nav.TrackerHistory, error) {
	out := nav.TrackerHistory{ReadAt: time.Now(), Currency: q.Currency, FXPolicyKey: q.FXPolicyKey, FXPolicyVersion: 1, FXMode: "market", BasisKey: r.basis}
	start, _ := time.Parse(time.DateOnly, q.From)
	end, _ := time.Parse(time.DateOnly, q.To)
	if q.PerformanceFrom == q.From {
		out.Baseline = &nav.TrackerHistoryPoint{Date: start.AddDate(0, 0, -1).Format(time.DateOnly), State: "ready", KnownValue: big.NewRat(1000, 1), StrategyFlow: new(big.Rat)}
	}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		out.Points = append(out.Points, nav.TrackerHistoryPoint{Date: day.Format(time.DateOnly), State: "ready", KnownValue: big.NewRat(1100, 1), StrategyFlow: new(big.Rat)})
	}
	return out, nil
}

func TestTrackerPerformanceUsesSignedChartFactsAndRechecksBasis(t *testing.T) {
	r := &trackerPerformanceStub{basis: strings.Repeat("a", 64)}
	s := NewInvestmentNAVServer(r, "synthetic-purpose-key")
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	ids := []string{"11111111-1111-4111-8111-111111111111"}
	page, e := s.GetTrackerHistory(ctx, &pb.GetTrackerHistoryRequest{LedgerIds: ids, Currency: "RUB", FxPolicyKey: "market", FromDate: "2024-01-01", ToDate: "2024-01-02", PerformanceFromDate: "2024-01-01"})
	if e != nil || page.PerformanceSnapshot == "" {
		t.Fatal(page, e)
	}
	request := &pb.GetTrackerPerformanceRequest{LedgerIds: ids, Currency: "RUB", FxPolicyKey: "market", FromDate: "2024-01-01", ToDate: "2024-01-02", SnapshotTokens: []string{page.PerformanceSnapshot}}
	result, e := s.GetTrackerPerformance(ctx, request)
	if e != nil || result.Profit.Numerator != "100" || result.Twr.Numerator != "1" || result.Twr.Denominator != "10" || result.HistoryScope != "recorded_securities_strategy" {
		t.Fatal(result, e)
	}
	if result.AnalyticsConvention != "retrospective_cashless_strategy_v1" || len(result.AnalyticsPoints) != 3 || result.AnalyticsPoints[0].Profit.Numerator != "0" || result.AnalyticsPoints[2].Profit.Numerator != "100" || len(result.MonthlyProfit) != 1 || result.MonthlyProfit[0].Profit.Numerator != "100" || len(result.YearlyProfit) != 1 {
		t.Fatal("daily and period analytics diverged from signed profit", result)
	}
	request.SnapshotTokens = append(request.SnapshotTokens, page.PerformanceSnapshot)
	if _, e = s.GetTrackerPerformance(ctx, request); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("repeated days accepted", e)
	}
	request.SnapshotTokens = request.SnapshotTokens[:1]
	other := ctxutil.WithUserID(ctx, "other")
	if _, e = s.GetTrackerPerformance(other, request); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("another actor's facts accepted", e)
	}
	r.basis = strings.Repeat("b", 64)
	if _, e = s.GetTrackerPerformance(ctx, request); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("corrected prices/journal accepted as unchanged", e)
	}
	if _, e = s.GetTrackerPerformance(context.Background(), request); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	if !investmentReadMethods[pb.InvestmentNAVService_GetTrackerPerformance_FullMethodName] {
		t.Fatal("summary must be permitted during read-only mode")
	}
}

func TestTrackerPerformanceRequiresEveryPageAndMatchingScope(t *testing.T) {
	r := &trackerPerformanceStub{basis: strings.Repeat("a", 64)}
	s := NewInvestmentNAVServer(r, "synthetic-page-key")
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	ids := []string{"11111111-1111-4111-8111-111111111111"}
	q := nav.TrackerHistoryRequest{LedgerIDs: ids, Currency: "RUB", FXPolicyKey: "market", From: "2024-01-01", To: "2024-02-15", PerformanceFrom: "2024-01-01"}
	firstData, _ := r.TrackerHistory(ctx, "tenant", "actor", nav.TrackerHistoryRequest{Currency: "RUB", FXPolicyKey: "market", From: "2024-01-01", To: "2024-01-31", PerformanceFrom: q.PerformanceFrom})
	first, e := s.trackerSigner.Seal("tenant", "actor", q, firstData)
	if e != nil {
		t.Fatal(e)
	}
	q.From = "2024-02-01"
	secondData, _ := r.TrackerHistory(ctx, "tenant", "actor", nav.TrackerHistoryRequest{Currency: "RUB", FXPolicyKey: "market", From: q.From, To: q.To})
	second, e := s.trackerSigner.Seal("tenant", "actor", q, secondData)
	if e != nil {
		t.Fatal(e)
	}
	req := &pb.GetTrackerPerformanceRequest{LedgerIds: ids, Currency: "RUB", FxPolicyKey: "market", FromDate: "2024-01-01", ToDate: q.To, SnapshotTokens: []string{first, second}}
	got, e := s.GetTrackerPerformance(ctx, req)
	if e != nil || got.Profit.GetNumerator() != "100" {
		t.Fatal(got, e)
	}
	for _, tokens := range [][]string{{first}, {second}, {second, first}, {first, second + "x"}} {
		req.SnapshotTokens = tokens
		if _, e = s.GetTrackerPerformance(ctx, req); status.Code(e) != codes.FailedPrecondition {
			t.Fatal("missing/reordered/tampered pages accepted", e)
		}
	}
	req.SnapshotTokens = []string{first, second}
	req.Currency = "USD"
	if _, e = s.GetTrackerPerformance(ctx, req); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("different currency accepted", e)
	}
	req.Currency = "RUB"
	req.FxPolicyKey = "cbr"
	if _, e = s.GetTrackerPerformance(ctx, req); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("different FX policy accepted", e)
	}
}

type trackerAnalyticsBenchmarkStub struct{ *trackerPerformanceStub }

func (r *trackerAnalyticsBenchmarkStub) TrackerBenchmarks(_ context.Context, _ string, _ string, q nav.TrackerHistoryRequest, base string) ([]nav.TrackerBenchmark, error) {
	first, _ := time.Parse(time.DateOnly, q.From)
	first = first.AddDate(0, 0, -1)
	last, _ := time.Parse(time.DateOnly, q.To)
	b := nav.TrackerBenchmark{Key: "imoex", BaseDate: base, Title: "IMOEX", Kind: "price_index", NativeCurrency: "RUB", Provider: "moex"}
	for day, i := first, int64(0); !day.After(last); day, i = day.AddDate(0, 0, 1), i+1 {
		b.Points = append(b.Points, nav.TrackerBenchmarkPoint{Date: day.Format(time.DateOnly), Index: big.NewRat(100+i*10, 1)})
	}
	return []nav.TrackerBenchmark{b}, nil
}
func TestTrackerMonetaryBenchmarkUsesSameSignedBaseline(t *testing.T) {
	r := &trackerAnalyticsBenchmarkStub{&trackerPerformanceStub{basis: strings.Repeat("a", 64)}}
	s := NewInvestmentNAVServer(r, "synthetic-key")
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	ids := []string{"11111111-1111-4111-8111-111111111111"}
	p, e := s.GetTrackerHistory(ctx, &pb.GetTrackerHistoryRequest{LedgerIds: ids, Currency: "RUB", FxPolicyKey: "market", FromDate: "2024-01-01", ToDate: "2024-01-02", PerformanceFromDate: "2024-01-01"})
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.GetTrackerPerformance(ctx, &pb.GetTrackerPerformanceRequest{LedgerIds: ids, Currency: "RUB", FxPolicyKey: "market", FromDate: "2024-01-01", ToDate: "2024-01-02", SnapshotTokens: []string{p.PerformanceSnapshot}})
	if e != nil || len(a.TrackerBenchmarks) != 1 {
		t.Fatal(a, e)
	}
	b := a.TrackerBenchmarks[0]
	if b.Points[0].VirtualValue.Numerator != "1000" || b.Points[2].VirtualValue.Numerator != "1200" || b.Points[2].VirtualProfit.Numerator != "200" || b.Points[2].VirtualReason != "" {
		t.Fatal(b)
	}
}
