package grpcadapter

import (
	"context"
	"errors"
	"math/big"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
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
	out := &pb.GetInvestmentExchangeRatesResponse{Currency: result.Currency, FxPolicyKey: result.PolicyKey, PolicyVersion: result.PolicyVersion, Mode: result.Mode, ReadAt: optionalInvestmentTimestamp(result.ReadAt), Source: result.Source, TimeConvention: result.TimeConvention}
	for _, rate := range result.Rates {
		row := &pb.InvestmentExchangeRate{Date: rate.Date, FromCurrency: rate.From, Rate: protoPerformanceAmount(rate.Rate)}
		out.Rates = append(out.Rates, row)
		if rate.Date != "" || rate.Rate == nil {
			continue
		}
		row.PreviousRate = protoPerformanceAmount(rate.PreviousRate)
		for _, leg := range rate.PreviousLegs {
			row.PreviousFxLegs = append(row.PreviousFxLegs, exchangeRateLeg(rate.From, leg, result.FXFixings))
		}
		for _, leg := range rate.CurrentLegs {
			out.CurrentFxLegs = append(out.CurrentFxLegs, exchangeRateLeg(rate.From, leg, result.FXFixings))
		}
	}
	return out, nil
}

func exchangeRateLeg(from string, leg calc.FXLeg, fixings []calc.FXFixing) *pb.InvestmentCurrentFXLeg {
	var observed *big.Rat
	for _, fixing := range fixings {
		if fixing.ID == leg.ID && fixing.Revision == leg.Revision && fixing.Policy == leg.Policy {
			observed, _ = new(big.Rat).SetString(fixing.Rate)
			break
		}
	}
	return &pb.InvestmentCurrentFXLeg{NativeCurrency: from, Id: leg.ID, Revision: leg.Revision, FromCurrency: leg.From, ToCurrency: leg.To, Policy: leg.Policy, Inverse: leg.Inverse, AsOf: optionalInvestmentTimestamp(leg.At), AvailableAt: optionalInvestmentTimestamp(leg.AvailableAt), ValidUntil: optionalInvestmentTimestamp(leg.ValidUntil), ObservedRate: protoPerformanceAmount(observed)}
}
