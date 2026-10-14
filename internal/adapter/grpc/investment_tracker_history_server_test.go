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

type trackerHistoryStub struct {
	InvestmentNAVRepo
	tenant, actor string
	err           error
}

func (r *trackerHistoryStub) TrackerHistory(_ context.Context, t, u string, q nav.TrackerHistoryRequest) (nav.TrackerHistory, error) {
	r.tenant, r.actor = t, u
	return nav.TrackerHistory{ReadAt: time.Now(), Currency: q.Currency, Points: []nav.TrackerHistoryPoint{{Date: q.From, State: "ready", KnownValue: big.NewRat(2, 3)}}}, r.err
}
func TestTrackerHistoryRPCScopeFractionAndReadOnly(t *testing.T) {
	r := &trackerHistoryStub{}
	s := NewInvestmentNAVServer(r)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	q := &pb.GetTrackerHistoryRequest{LedgerIds: []string{"11111111-1111-4111-8111-111111111111"}, Currency: "RUB", FxPolicyKey: "market", FromDate: "2020-01-01", ToDate: "2020-01-02"}
	result, e := s.GetTrackerHistory(ctx, q)
	if e != nil || r.tenant != "tenant" || r.actor != "actor" || result.Points[0].KnownValue.Denominator != "3" || result.ReportingTimezone != "Europe/Moscow" {
		t.Fatal(result, e)
	}
	if _, e = s.GetTrackerHistory(context.Background(), q); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	r.err = errors.New("private database details")
	if _, e = s.GetTrackerHistory(ctx, q); status.Code(e) != codes.Internal || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
	if !investmentReadMethods[pb.InvestmentNAVService_GetTrackerHistory_FullMethodName] {
		t.Fatal("history must remain a read")
	}
}
