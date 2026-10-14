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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type quotesRepoStub struct {
	InvestmentNAVRepo
	tenant, actor string
	keys          []string
	err           error
}

func (r *quotesRepoStub) InvestmentQuotes(_ context.Context, tenant, actor string, keys []string) (nav.PortfolioQuotes, error) {
	r.tenant, r.actor, r.keys = tenant, actor, keys
	return nav.PortfolioQuotes{ReadAt: time.Now(), Quotes: []nav.PortfolioQuote{{InstrumentKey: "TEST", Currency: "USD", CleanPrice: big.NewRat(2, 3), State: "fresh"}, {InstrumentKey: "MISSING", State: "unavailable"}}}, r.err
}
func TestInvestmentQuotesRPCScopePrecisionReadOnlyAndSanitizedErrors(t *testing.T) {
	repo := &quotesRepoStub{}
	server := NewInvestmentNAVServer(repo)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	req := &pb.GetInvestmentQuotesRequest{InstrumentKeys: []string{"TEST", "MISSING"}}
	result, e := server.GetInvestmentQuotes(ctx, req)
	if e != nil || repo.tenant != "tenant" || repo.actor != "actor" || len(repo.keys) != 2 || result.Quotes[0].CleanPrice.Denominator != "3" || result.Quotes[1].CleanPrice != nil {
		t.Fatal(result, e)
	}
	if _, e = server.GetInvestmentQuotes(context.Background(), req); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	if _, e = server.GetInvestmentQuotes(ctx, &pb.GetInvestmentQuotesRequest{InstrumentKeys: make([]string, 501)}); status.Code(e) != codes.ResourceExhausted {
		t.Fatal(e)
	}
	repo.err = d.ErrInvestmentForbidden
	if _, e = server.GetInvestmentQuotes(ctx, req); status.Code(e) != codes.PermissionDenied {
		t.Fatal(e)
	}
	repo.err = errors.New("private database detail")
	if _, e = server.GetInvestmentQuotes(ctx, req); status.Code(e) != codes.Internal || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
	if !investmentReadMethods[pb.InvestmentNAVService_GetInvestmentQuotes_FullMethodName] {
		t.Fatal("quote read classified as write")
	}
}
