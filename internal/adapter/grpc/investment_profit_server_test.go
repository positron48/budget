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

type profitRepoStub struct {
	InvestmentNAVRepo
	tenant, actor string
	value         nav.ProfitAnalytics
	err           error
}

func (r *profitRepoStub) ProfitForSeries(_ context.Context, tenant, actor, key, from, to string) (nav.ProfitAnalytics, error) {
	r.tenant, r.actor = tenant, actor
	return r.value, r.err
}
func TestProfitRPCPrecisionScopeAndUnknowns(t *testing.T) {
	repo := &profitRepoStub{value: nav.ProfitAnalytics{Performance: nav.Performance{PeriodPerformance: calc.PeriodPerformance{Profit: big.NewRat(1, 3)}}, Timeline: calc.ProfitTimeline{Points: []calc.ProfitPoint{{Date: "2024-01-01", Cumulative: big.NewRat(1, 3)}, {Date: "2024-01-02", Cumulative: big.NewRat(99, 1), CumulativeReason: "terminal_nav_unknown"}}, Months: []calc.ProfitBucket{{Period: "2024-01", Profit: big.NewRat(99, 1), Reason: "history_reset"}}}, Attribution: calc.ProfitAttribution{Components: []calc.ProfitComponent{{Kind: "fees", Known: big.NewRat(-1, 7), Complete: true}}}}}
	repo.value.Instruments = nav.InstrumentContributions{Complete: true, Rows: []nav.InstrumentContribution{{InstrumentKey: "sold", LedgerIDs: []string{"a", "b"}, Closed: true, Known: big.NewRat(1, 3), Share: big.NewRat(1, 7), Complete: true}}, Remainder: calc.ProfitComponent{Kind: "cash_and_unallocated", Known: new(big.Rat), Complete: true}}
	server := NewInvestmentNAVServer(repo)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	out, e := server.GetInvestmentProfit(ctx, &pb.GetInvestmentProfitRequest{SeriesKey: "series"})
	if e != nil || repo.tenant != "tenant" || repo.actor != "actor" || out.Profit.Denominator != "3" || out.Points[0].Cumulative.Denominator != "3" || out.Points[1].Cumulative != nil || out.Months[0].Profit != nil || out.Components[0].Known.Denominator != "7" {
		t.Fatal(out, e)
	}
	if !out.InstrumentsComplete || len(out.Instruments) != 1 || out.Instruments[0].Known.Denominator != "3" || out.Instruments[0].Share.Denominator != "7" || !out.Instruments[0].Closed || out.InstrumentRemainder.Known.Numerator != "0" {
		t.Fatal(out)
	}
	repo.value.Instruments.Complete = false
	out, e = server.GetInvestmentProfit(ctx, nil)
	if e != nil || out.Instruments[0].Share != nil || out.InstrumentRemainder.Known != nil {
		t.Fatal(out, e)
	}
	if _, e = server.GetInvestmentProfit(context.Background(), nil); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	repo.err = errors.New("private database credentials")
	if _, e = server.GetInvestmentProfit(ctx, nil); status.Code(e) != codes.Internal || strings.Contains(e.Error(), "credentials") {
		t.Fatal(e)
	}
}
