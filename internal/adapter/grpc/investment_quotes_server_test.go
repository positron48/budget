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

type quotesRepoStub struct {
	InvestmentNAVRepo
	tenant, actor string
	keys          []string
	err           error
}

func (r *quotesRepoStub) InvestmentQuotes(_ context.Context, tenant, actor string, keys []string) (nav.PortfolioQuotes, error) {
	r.tenant, r.actor, r.keys = tenant, actor, keys
	return nav.PortfolioQuotes{ReadAt: time.Now(), Quotes: []nav.PortfolioQuote{{PreviousClose: big.NewRat(1, 3), PreviousCloseCurrency: "USD", PreviousCloseAt: time.Now().Add(-24 * time.Hour), InstrumentKey: "TEST", Currency: "USD", CleanPrice: big.NewRat(2, 3), State: "fresh", Instrument: &md.Instrument{ID: "synthetic-id", ISIN: "SYNTHETIC", ClassCode: "TEST"}, MetadataAt: time.Now(), RawPrice: big.NewRat(95, 1), RawUnit: "percent_of_nominal", Nominal: big.NewRat(800, 1), NominalCurrency: "USD", Accrued: big.NewRat(0, 1), AccruedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, {InstrumentKey: "MISSING", State: "unavailable"}}}, r.err
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
	q := result.Quotes[0]
	if q.PreviousClose.GetNumerator() != "1" || q.PreviousClose.GetDenominator() != "3" || q.PreviousCloseCurrency != "USD" || q.PreviousCloseAt == nil {
		t.Fatal("previous close lost", q)
	}
	if q.Instrument == nil || q.Instrument.Isin != "SYNTHETIC" || q.Instrument.ClassCode != "TEST" || q.MetadataAt == nil || q.RawPrice.GetNumerator() != "95" || q.RawUnit != "percent_of_nominal" || q.Nominal.GetNumerator() != "800" || q.NominalCurrency != "USD" || q.Accrued.GetNumerator() != "0" || q.AccruedAt == nil || result.Quotes[1].Instrument != nil {
		t.Fatal("metadata/known-zero bond details lost", result)
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
