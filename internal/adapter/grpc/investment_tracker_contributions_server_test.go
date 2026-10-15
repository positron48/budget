package grpcadapter

import (
	"context"
	"errors"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math/big"
	"strings"
	"testing"
)

type contributionStub struct {
	InvestmentNAVRepo
	tenant, actor string
	request       nav.TrackerHistoryRequest
	err           error
}

func (r *contributionStub) TrackerHistory(_ context.Context, t, u string, q nav.TrackerHistoryRequest) (nav.TrackerHistory, error) {
	r.tenant, r.actor, r.request = t, u, q
	return nav.TrackerHistory{Currency: q.Currency, FXPolicyKey: q.FXPolicyKey, FXPolicyVersion: 1, FXMode: "market", BasisKey: strings.Repeat("a", 64), Baseline: &nav.TrackerHistoryPoint{State: "ready"}, Points: []nav.TrackerHistoryPoint{{State: "ready"}}, Contributions: []nav.TrackerContribution{{LedgerID: q.LedgerIDs[0], InstrumentKey: "S", Profit: big.NewRat(53, 1), InScope: true}, {LedgerID: q.LedgerIDs[0], Profit: big.NewRat(-3, 1), InScope: false}}}, r.err
}
func TestTrackerContributionsScopeReadOnlyAndUnallocatedIsolation(t *testing.T) {
	r := &contributionStub{}
	s := NewInvestmentNAVServer(r)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	q := &pb.GetTrackerContributionsRequest{LedgerIds: []string{"11111111-1111-4111-8111-111111111111"}, GroupIds: []string{"22222222-2222-4222-8222-222222222222"}, Currency: "RUB", FxPolicyKey: "market", FromDate: "2024-01-01", ToDate: "2024-01-03"}
	p, e := s.GetTrackerContributions(ctx, q)
	if e != nil || !r.request.ContributionOnly || r.request.PerformanceFrom != "" || r.tenant != "tenant" || r.actor != "actor" || p.Profit.Numerator != "53" || p.Rows[0].ProfitShare.Numerator != "1" || p.Rows[1].ProfitShare != nil {
		t.Fatal(p, e)
	}
	if _, e = s.GetTrackerContributions(context.Background(), q); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	r.err = errors.New("private database details")
	if _, e = s.GetTrackerContributions(ctx, q); status.Code(e) != codes.Internal || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
	if !investmentReadMethods[pb.InvestmentNAVService_GetTrackerContributions_FullMethodName] {
		t.Fatal("contributions must remain read only")
	}
}

func TestContributionSharesHaveBoundedPresentationPrecision(t *testing.T) {
	r := contributionProfitShare(big.NewRat(1, 1), big.NewRat(3, 1))
	if r.RatString() != "333333333333/1000000000000" {
		t.Fatal(r)
	}
	tiny, _ := new(big.Rat).SetString("0.00000000000000000001")
	if contributionProfitShare(big.NewRat(1, 1), tiny) != nil {
		t.Fatal("near-zero result produced an unbounded percentage")
	}
}
