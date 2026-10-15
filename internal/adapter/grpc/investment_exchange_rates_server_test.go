package grpcadapter

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	calc "github.com/positron48/budget/internal/usecase/investmentcalc"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ratesRepoStub struct {
	InvestmentNAVRepo
	tenant, actor, key, currency string
	sources, dates               []string
	err                          error
	result                       *nav.ExchangeRates
}

func (r *ratesRepoStub) InvestmentExchangeRates(_ context.Context, tenant, actor, key, currency string, sources, dates []string) (nav.ExchangeRates, error) {
	r.tenant, r.actor, r.key, r.currency, r.sources, r.dates = tenant, actor, key, currency, sources, dates
	if r.result != nil {
		return *r.result, r.err
	}
	return nav.ExchangeRates{Currency: currency, PolicyKey: key, PolicyVersion: 2, Mode: "market", ReadAt: time.Now(), Rates: []nav.ExchangeRate{{Date: "2020-01-01", From: "USD", Rate: big.NewRat(2, 3)}, {From: "USD"}}}, r.err
}

func TestExchangeRatesRPCScopePrecisionUnknownsAndErrors(t *testing.T) {
	repo := &ratesRepoStub{}
	server := NewInvestmentNAVServer(repo)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	req := &pb.GetInvestmentExchangeRatesRequest{FxPolicyKey: "market", Currency: "RUB", FromCurrencies: []string{"USD"}, Dates: []string{"2020-01-01"}}
	out, e := server.GetInvestmentExchangeRates(ctx, req)
	if e != nil || repo.tenant != "tenant" || repo.actor != "actor" || repo.key != "market" || repo.currency != "RUB" || len(repo.dates) != 1 || len(repo.sources) != 1 || out.Rates[0].Rate.Denominator != "3" || out.Rates[1].Rate != nil || out.PolicyVersion != 2 {
		t.Fatal(out, e)
	}
	if _, e = server.GetInvestmentExchangeRates(context.Background(), req); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	for _, c := range []struct {
		err  error
		code codes.Code
	}{{d.ErrInvestmentForbidden, codes.PermissionDenied}, {d.ErrInvestmentInvalid, codes.InvalidArgument}, {md.ErrNotFound, codes.NotFound}, {errors.New("private connection detail"), codes.Internal}} {
		repo.err = c.err
		if _, e = server.GetInvestmentExchangeRates(ctx, req); status.Code(e) != c.code || strings.Contains(e.Error(), "private") {
			t.Fatal(e)
		}
	}
	if !investmentReadMethods[pb.InvestmentNAVService_GetInvestmentExchangeRates_FullMethodName] {
		t.Fatal("report requires writes in read-only mode")
	}
}

func TestCurrentExchangeRatesRPCExposesObservedInverseEvidence(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fixing := calc.FXFixing{ID: "fixing", Revision: 2, Policy: "alpha@3", From: "USD", To: "RUB", Rate: "80", At: at, AvailableAt: at.Add(24 * time.Hour), ValidUntil: at.Add(96 * time.Hour)}
	leg := calc.FXLeg{ID: fixing.ID, Revision: fixing.Revision, Policy: fixing.Policy, From: fixing.From, To: fixing.To, At: fixing.At, AvailableAt: fixing.AvailableAt, ValidUntil: fixing.ValidUntil, Inverse: true}
	repo := &ratesRepoStub{result: &nav.ExchangeRates{Currency: "USD", PolicyKey: "alpha", PolicyVersion: 3, Mode: "market", Source: "alphavantage", TimeConvention: "Retrospective next-day close", ReadAt: at.Add(48 * time.Hour), FXFixings: []calc.FXFixing{fixing}, Rates: []nav.ExchangeRate{{From: "RUB", Rate: big.NewRat(1, 80), CurrentLegs: []calc.FXLeg{leg}}, {Date: "2026-01-02", From: "RUB", Rate: big.NewRat(1, 80), CurrentLegs: []calc.FXLeg{leg}}, {From: "EUR", CurrentLegs: []calc.FXLeg{leg}}}}}
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	out, err := NewInvestmentNAVServer(repo).GetInvestmentExchangeRates(ctx, &pb.GetInvestmentExchangeRatesRequest{FxPolicyKey: "alpha", Currency: "USD"})
	if err != nil || out.Source != "alphavantage" || out.TimeConvention != "Retrospective next-day close" || len(out.CurrentFxLegs) != 1 {
		t.Fatal(out, err)
	}
	observed := out.CurrentFxLegs[0]
	if !observed.Inverse || observed.NativeCurrency != "RUB" || observed.FromCurrency != "USD" || observed.ToCurrency != "RUB" || observed.ObservedRate.Numerator != "80" || observed.ObservedRate.Denominator != "1" || observed.Revision != 2 || observed.AsOf.Seconds != at.Unix() || observed.AvailableAt.Seconds != at.Add(24*time.Hour).Unix() || observed.ValidUntil.Seconds != at.Add(96*time.Hour).Unix() {
		t.Fatal("original fixing replaced by converted or read time", observed)
	}
}

func TestCurrentExchangeRatesRPCSeparatesPreviousEvidenceFromDatedRows(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	previous := calc.FXFixing{ID: "previous", Revision: 2, Policy: "alpha@1", From: "USD", To: "RUB", Rate: "75", At: at, AvailableAt: at.Add(time.Hour), ValidUntil: at.Add(24 * time.Hour)}
	leg := calc.FXLeg{ID: previous.ID, Revision: previous.Revision, Policy: previous.Policy, From: previous.From, To: previous.To, At: previous.At, AvailableAt: previous.AvailableAt, ValidUntil: previous.ValidUntil}
	row := nav.ExchangeRate{From: "USD", Rate: big.NewRat(80, 1), PreviousRate: big.NewRat(75, 1), PreviousLegs: []calc.FXLeg{leg}}
	dated := row
	dated.Date = "2026-01-01"
	repo := &ratesRepoStub{result: &nav.ExchangeRates{Currency: "RUB", PolicyKey: "alpha", PolicyVersion: 1, FXFixings: []calc.FXFixing{previous}, Rates: []nav.ExchangeRate{row, dated}}}
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	out, err := NewInvestmentNAVServer(repo).GetInvestmentExchangeRates(ctx, &pb.GetInvestmentExchangeRatesRequest{})
	if err != nil || out.Rates[0].PreviousRate.GetNumerator() != "75" || len(out.Rates[0].PreviousFxLegs) != 1 || out.Rates[0].PreviousFxLegs[0].ObservedRate.GetNumerator() != "75" || out.Rates[1].PreviousRate != nil || len(out.CurrentFxLegs) != 0 {
		t.Fatal(out, err)
	}
}
