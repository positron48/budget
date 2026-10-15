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
	"time"
)

type trackerInstrumentStub struct {
	InvestmentNAVRepo
	tenant, actor string
	err           error
}

func (r *trackerInstrumentStub) TrackerInstrumentPrices(_ context.Context, t, u string, q nav.TrackerInstrumentRequest) (nav.TrackerInstrumentHistory, error) {
	r.tenant, r.actor = t, u
	return nav.TrackerInstrumentHistory{ReadAt: time.Now(), Currency: q.Currency, Points: []nav.TrackerInstrumentPoint{{Date: q.From, State: "ready", Raw: big.NewRat(90, 1), NativeClean: big.NewRat(2, 3)}}}, r.err
}
func TestTrackerInstrumentRPCScopeFractionAndReadOnly(t *testing.T) {
	r := &trackerInstrumentStub{}
	s := NewInvestmentNAVServer(r)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	q := &pb.GetTrackerInstrumentPricesRequest{InstrumentKey: "S", Currency: "RUB", FxPolicyKey: "market", FromDate: "2020-01-01", ToDate: "2020-01-02"}
	result, e := s.GetTrackerInstrumentPrices(ctx, q)
	if e != nil || r.tenant != "tenant" || r.actor != "actor" || result.Points[0].NativeClean.Denominator != "3" || result.ReportingTimezone != "Europe/Moscow" {
		t.Fatal(result, e)
	}
	if _, e = s.GetTrackerInstrumentPrices(context.Background(), q); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	r.err = errors.New("private database details")
	if _, e = s.GetTrackerInstrumentPrices(ctx, q); status.Code(e) != codes.Internal || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
	if !investmentReadMethods[pb.InvestmentNAVService_GetTrackerInstrumentPrices_FullMethodName] {
		t.Fatal("history must remain a read")
	}
}
