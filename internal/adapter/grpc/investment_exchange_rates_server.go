package grpcadapter

import (
	"context"
	"errors"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type investmentExchangeRatesRepo interface {
	InvestmentExchangeRates(context.Context, string, string, string, string, []string, []string) (nav.ExchangeRates, error)
}

func (s *InvestmentNAVServer) GetInvestmentExchangeRates(ctx context.Context, req *pb.GetInvestmentExchangeRatesRequest) (*pb.GetInvestmentExchangeRatesResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	repo, ok := s.repo.(investmentExchangeRatesRepo)
	if !ok {
		return nil, status.Error(codes.Unavailable, "portfolio exchange rates unavailable")
	}
	result, e := repo.InvestmentExchangeRates(ctx, tenant, actor, req.GetFxPolicyKey(), req.GetCurrency(), req.GetFromCurrencies(), req.GetDates())
	if e != nil {
		if errors.Is(e, d.ErrInvestmentForbidden) || errors.Is(e, d.ErrInvestmentInvalid) {
			return nil, investmentRPCError(e)
		}
		return nil, marketRPCError(e)
	}
	out := &pb.GetInvestmentExchangeRatesResponse{Currency: result.Currency, FxPolicyKey: result.PolicyKey, PolicyVersion: result.PolicyVersion, Mode: result.Mode, ReadAt: optionalInvestmentTimestamp(result.ReadAt)}
	for _, rate := range result.Rates {
		out.Rates = append(out.Rates, &pb.InvestmentExchangeRate{Date: rate.Date, FromCurrency: rate.From, Rate: protoPerformanceAmount(rate.Rate)})
	}
	return out, nil
}
