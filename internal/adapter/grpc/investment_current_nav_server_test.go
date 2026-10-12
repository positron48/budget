package grpcadapter

import (
	"context"
	"errors"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math/big"
	"strings"
	"testing"
	"time"
)

type currentNAVRepoStub struct {
	InvestmentNAVRepo
	v                  nav.CurrentNAV
	err                error
	tenant, actor, key string
}

func (r *currentNAVRepoStub) CurrentNAVForSeries(_ context.Context, tenant, actor, key string) (nav.CurrentNAV, error) {
	r.tenant, r.actor, r.key = tenant, actor, key
	return r.v, r.err
}
func TestCurrentNAVRPCProvisionalExactAndAuthorized(t *testing.T) {
	at := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	result := nav.Result{Currency: "USD", At: at, LedgerVersions: map[string]int64{"b": 2, "a": 1}, FXPolicy: "market@2"}
	result.Complete = false
	result.KnownValue = big.NewRat(5, 3)
	result.Issues = []calc.QualityIssue{{ComponentID: "p", Code: "price_stale"}}
	result.FXPaths = map[string][]calc.FXLeg{"RUB": {{ID: "fixing", Revision: 2, Policy: "market@2", From: "USD", To: "RUB", Inverse: true}}}
	q := nav.CurrentQuote{InstrumentKey: "ABC", Scope: md.Scope{Provider: "tinvest", AccessScope: "personal"}, MaxAge: time.Hour, Price: md.SelectedPrice{State: "stale", Candidate: &calc.PriceFact{Price: "12.345", Currency: "RUB", Unit: "currency_per_unit", EffectiveAt: at.Add(-2 * time.Hour)}, ObservationID: "observation", ObservationRevision: 3}}
	r := &currentNAVRepoStub{v: nav.CurrentNAV{SeriesKey: "series", LastCompletedDate: "2024-01-01", Result: result, Quotes: []nav.CurrentQuote{q}, FXFixings: []calc.FXFixing{{ID: "fixing", Revision: 2, Policy: "market@2", From: "USD", To: "RUB", Rate: "20"}}}}
	s := NewInvestmentNAVServer(r)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	out, e := s.GetInvestmentCurrentNAV(ctx, &pb.GetInvestmentCurrentNAVRequest{SeriesKey: "series"})
	if e != nil || !out.Provisional || out.Complete || out.KnownValue.Numerator != "5" || out.KnownValue.Denominator != "3" || out.Quotes[0].CandidatePrice.Numerator != "2469" || out.Quotes[0].CandidatePrice.Denominator != "200" || out.FxLegs[0].ObservedRate.Numerator != "20" || !out.FxLegs[0].Inverse || out.Quotes[0].ValidUntil.Seconds != at.Add(-time.Hour).Unix() || strings.Join(out.LedgerIds, ",") != "a,b" || r.tenant != "tenant" || r.actor != "actor" || r.key != "series" {
		t.Fatal(out, e, r)
	}
	if _, e = s.GetInvestmentCurrentNAV(context.Background(), &pb.GetInvestmentCurrentNAVRequest{}); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	r.err = errors.New("private source data")
	if _, e = s.GetInvestmentCurrentNAV(ctx, &pb.GetInvestmentCurrentNAVRequest{}); status.Code(e) != codes.Internal || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
	if !investmentReadMethods["/budget.v1.InvestmentNAVService/GetInvestmentCurrentNAV"] {
		t.Fatal("read-only endpoint blocked")
	}
}
