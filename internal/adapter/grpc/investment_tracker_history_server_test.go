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
	"time"
)

type trackerHistoryStub struct {
	InvestmentNAVRepo
	tenant, actor string
	err           error
	request       nav.TrackerHistoryRequest
	value         *nav.TrackerHistory
}

func (r *trackerHistoryStub) TrackerHistory(_ context.Context, t, u string, q nav.TrackerHistoryRequest) (nav.TrackerHistory, error) {
	r.tenant, r.actor, r.request = t, u, q
	if r.value != nil {
		return *r.value, r.err
	}
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

func TestTrackerHistoryBreakdownPreservesNativeFractionsAndFXEvidence(t *testing.T) {
	at := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	leg := calc.FXLeg{ID: "fx", Revision: 2, From: "USD", To: "RUB", Policy: "market@1", At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour)}
	r := &trackerHistoryStub{value: &nav.TrackerHistory{Currency: "RUB", FXPolicyKey: "market", FXPolicyVersion: 1, FXMode: "market", Points: []nav.TrackerHistoryPoint{{Date: "2020-01-02", State: "ready", KnownValue: big.NewRat(20, 1), Details: []nav.TrackerValueDetail{{LedgerID: "ledger", InstrumentKey: "S", Currency: "USD", Quantity: big.NewRat(2, 1), Price: big.NewRat(3, 2), NativeValue: big.NewRat(3, 1), Value: big.NewRat(20, 1), FXRate: big.NewRat(20, 3), PriceAt: at, Source: "moex/personal", MappingVersion: 1}}, DetailFX: map[string][]calc.FXLeg{"USD": {leg}}, DetailFixings: []calc.FXFixing{{ID: "fx", Revision: 2, Policy: "market@1", Rate: "6.666666666666"}}}}}}
	s := NewInvestmentNAVServer(r)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	q := &pb.GetTrackerHistoryRequest{LedgerIds: []string{"11111111-1111-4111-8111-111111111111"}, Currency: "RUB", FxPolicyKey: "market", FromDate: "2020-01-01", ToDate: "2020-01-03", DetailDate: "2020-01-02"}
	out, e := s.GetTrackerHistory(ctx, q)
	if e != nil || !out.DetailsIncluded || r.request.DetailDate != q.DetailDate || r.request.From != q.FromDate || r.request.To != q.ToDate || len(out.Points) != 1 {
		t.Fatal(out, e)
	}
	p := out.Points[0]
	if p.Details[0].UnitPrice.Denominator != "2" || p.Details[0].FxRate.Denominator != "3" || p.FxLegs[0].ObservedRate.Denominator != "500000000000" || p.FxLegs[0].Revision != 2 || p.FxLegs[0].ValidUntil.Seconds != at.Add(24*time.Hour).Unix() {
		t.Fatal(p)
	}
	if out.PerformanceSnapshot != "" {
		t.Fatal("breakdown unexpectedly signed as a full chart page")
	}
}
