package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math/big"
	"testing"
)

type compositionRepoStub struct {
	InvestmentNAVRepo
	tenant, actor string
	result        nav.CompositionHistory
}

func (r *compositionRepoStub) InvestmentCompositionHistory(_ context.Context, tenant, actor, key, from, to string, selected []string, dimension string) (nav.CompositionHistory, error) {
	r.tenant = tenant
	r.actor = actor
	return r.result, nil
}
func TestCompositionRPCDoesNotLeakStaleValuesOrPriorDatePie(t *testing.T) {
	c := &nav.Composition{SecuritiesKnown: big.NewRat(1, 3), CashKnown: big.NewRat(10, 1), SecuritiesComplete: true, CashComplete: true, Complete: true, Segments: []nav.CompositionSegment{{Kind: "cash", KnownValue: big.NewRat(10, 1), Complete: true, Weight: big.NewRat(1, 1)}}}
	repo := &compositionRepoStub{result: nav.CompositionHistory{Classification: nav.InstrumentGroups{Version: 9007199254740993}, Currency: "RUB", Days: []nav.CompositionDay{{DailyNAV: nav.DailyNAV{Date: "2024-01-01", State: "ready"}, Composition: c}, {DailyNAV: nav.DailyNAV{Date: "2024-01-02", State: "stale"}, Composition: c}}}}
	server := NewInvestmentNAVServer(repo)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	out, e := server.GetInvestmentCompositionHistory(ctx, &pb.GetInvestmentCompositionHistoryRequest{SeriesKey: "series", FromDate: "2024-01-01", ToDate: "2024-01-02"})
	if e != nil || repo.tenant != "tenant" || repo.actor != "actor" || out.ClassificationVersion != 9007199254740993 || out.Points[0].SecuritiesKnown.Numerator != "1" || out.Points[0].SecuritiesKnown.Denominator != "3" || out.Points[1].SecuritiesKnown != nil || out.Complete || len(out.Segments) != 0 {
		t.Fatal(out, e)
	}
	if _, e = server.GetInvestmentCompositionHistory(context.Background(), nil); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
}
