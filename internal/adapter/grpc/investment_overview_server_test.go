package grpcadapter

import (
	"context"
	"errors"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math/big"
	"strings"
	"testing"
)

type overviewRepoStub struct {
	InvestmentNAVRepo
	value                        nav.Overview
	tenant, actor, key, from, to string
	err                          error
}

func (r *overviewRepoStub) OverviewForSeries(_ context.Context, tenant, actor, key, from, to string) (nav.Overview, error) {
	r.tenant, r.actor, r.key, r.from, r.to = tenant, actor, key, from, to
	return r.value, r.err
}
func TestOverviewRPCSnapshotPrecisionAndUnknowns(t *testing.T) {
	r := &overviewRepoStub{value: nav.Overview{Performance: nav.Performance{SeriesKey: "series", Currency: "USD"}, NAV: nav.DailyNAV{State: "incomplete", Result: &nav.Result{PortfolioValue: calc.PortfolioValue{Valuation: calc.Valuation{KnownValue: big.NewRat(2, 3)}}}}, Composition: &nav.Composition{SecuritiesKnown: big.NewRat(2, 3), CashKnown: big.NewRat(0, 1), CashComplete: true}, Daily: calc.ProfitPoint{Daily: big.NewRat(1, 3)}, FX: []nav.OverviewFX{{From: "USD", To: "RUB", State: "expired", Rate: big.NewRat(7, 3)}}}}
	s := NewInvestmentNAVServer(r)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	out, e := s.GetInvestmentOverview(ctx, &pb.GetInvestmentOverviewRequest{SeriesKey: "series", FromDate: "2024-01-01", ToDate: "2024-01-02"})
	if e != nil || r.tenant != "tenant" || r.actor != "actor" || r.key != "series" || r.from != "2024-01-01" || r.to != "2024-01-02" || out.Nav.KnownValue.Denominator != "3" || out.SecuritiesComplete || !out.CashComplete || out.CashKnown.Numerator != "0" || out.DailyProfit.Denominator != "3" || out.Fx[0].State != "expired" || out.Scope != "full_registered_accounts" {
		t.Fatal(out, e, r)
	}
	r.value.NAV.State = "stale"
	r.value.Daily.DailyReason = "terminal_nav_unknown"
	r.value.Performance.Reason = "terminal_nav_unknown"
	r.value.Performance.Profit = big.NewRat(999, 1)
	out, e = s.GetInvestmentOverview(ctx, nil)
	if e != nil || out.Nav.KnownValue != nil || out.SecuritiesKnown != nil || out.CashKnown != nil || out.DailyProfit != nil || out.Performance.Profit != nil {
		t.Fatal("stale or unknown value leaked", out, e)
	}
	if _, e = s.GetInvestmentOverview(context.Background(), nil); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	r.err = errors.New("private database credentials")
	if _, e = s.GetInvestmentOverview(ctx, nil); status.Code(e) != codes.Internal || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
}
