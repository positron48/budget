package grpcadapter

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

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
}

func (r *ratesRepoStub) InvestmentExchangeRates(_ context.Context, tenant, actor, key, currency string, sources, dates []string) (nav.ExchangeRates, error) {
	r.tenant, r.actor, r.key, r.currency, r.sources, r.dates = tenant, actor, key, currency, sources, dates
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
