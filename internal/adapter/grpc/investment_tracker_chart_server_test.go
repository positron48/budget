package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"strings"
	"testing"
)

type chartStub struct {
	trackerPerformanceStub
	saved             map[string][]byte
	forbidden         bool
	historyCalls      int
	benchmarkRevision string
}

func (r *chartStub) TrackerHistory(ctx context.Context, t, a string, q nav.TrackerHistoryRequest) (nav.TrackerHistory, error) {
	r.historyCalls++
	if r.forbidden {
		return nav.TrackerHistory{}, d.ErrInvestmentForbidden
	}
	return r.trackerPerformanceStub.TrackerHistory(ctx, t, a, q)
}
func (r *chartStub) TrackerBenchmarks(context.Context, string, string, nav.TrackerHistoryRequest, string) ([]nav.TrackerBenchmark, error) {
	return []nav.TrackerBenchmark{{Key: "imoex", Reason: r.benchmarkRevision}}, nil
}
func (r *chartStub) ReadTrackerChart(_ context.Context, t, a, k, b string) ([]byte, error) {
	return r.saved[t+a+k+b], nil
}
func (r *chartStub) SaveTrackerChart(_ context.Context, t, a, k, b string, _, v []byte, _ bool) error {
	r.saved[t+a+k+b] = append([]byte(nil), v...)
	return nil
}
func (r *chartStub) TrackerChartsToRefresh(context.Context) ([]nav.TrackerChartRefresh, error) {
	return nil, nil
}
func TestTrackerChartPersistedProjectionRechecksMembershipAndBothBases(t *testing.T) {
	r := &chartStub{trackerPerformanceStub: trackerPerformanceStub{basis: strings.Repeat("a", 64)}, saved: map[string][]byte{}}
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	req := &pb.GetTrackerChartRequest{LedgerIds: []string{"11111111-1111-4111-8111-111111111111"}, Currency: "RUB", FxPolicyKey: "market", FromDate: "2024-01-01", ToDate: "2024-01-02"}
	s := NewInvestmentNAVServer(r, "test-chart-key")
	first, e := s.GetTrackerChart(ctx, req)
	if e != nil || first.CacheHit || first.Portfolio.Performance.Profit.Numerator != "100" {
		t.Fatal(first, e)
	}
	for _, p := range first.Portfolio.Pages {
		if p.PerformanceSnapshot != "" {
			t.Fatal("persisted snapshot capability")
		}
	}
	s = NewInvestmentNAVServer(r, "different-key-after-restart")
	before := r.historyCalls
	warm, e := s.GetTrackerChart(ctx, req)
	if e != nil || !warm.CacheHit || r.historyCalls-before != 1 {
		t.Fatal("daily series replayed after restart", e, r.historyCalls-before)
	}
	r.benchmarkRevision = "corrected"
	out, e := s.GetTrackerChart(ctx, req)
	if e != nil || out.CacheHit {
		t.Fatal("benchmark correction not fenced", e)
	}
	r.basis = strings.Repeat("b", 64)
	out, e = s.GetTrackerChart(ctx, req)
	if e != nil || out.CacheHit {
		t.Fatal("journal correction not fenced", e)
	}
	other := ctxutil.WithUserID(ctx, "other")
	out, e = s.GetTrackerChart(other, req)
	if e != nil || out.CacheHit {
		t.Fatal("another actor reused projection", e)
	}
	r.forbidden = true
	if _, e = s.GetTrackerChart(ctx, req); e == nil {
		t.Fatal("revoked member received projection")
	}
}
