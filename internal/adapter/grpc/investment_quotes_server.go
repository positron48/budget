package grpcadapter

import (
	"context"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type investmentQuotesRepo interface {
	InvestmentQuotes(context.Context, string, string, []string) (nav.PortfolioQuotes, error)
}

func (s *InvestmentNAVServer) GetInvestmentQuotes(ctx context.Context, req *pb.GetInvestmentQuotesRequest) (*pb.GetInvestmentQuotesResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	if _, e = nav.NormalizeQuoteKeys(req.GetInstrumentKeys()); e != nil {
		return nil, investmentRPCError(e)
	}
	repo, ok := s.repo.(investmentQuotesRepo)
	if !ok {
		return nil, status.Error(codes.Unavailable, "portfolio quotes unavailable")
	}
	result, e := repo.InvestmentQuotes(ctx, tenant, actor, req.GetInstrumentKeys())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentQuotesResponse{ReadAt: optionalInvestmentTimestamp(result.ReadAt)}
	for _, q := range result.Quotes {
		out.Quotes = append(out.Quotes, &pb.InvestmentQuote{InstrumentKey: q.InstrumentKey, Currency: q.Currency, CleanPrice: protoPerformanceAmount(q.CleanPrice), PriceAt: optionalInvestmentTimestamp(q.At), Source: q.Source, State: q.State, Issues: q.Issues, MappingVersion: q.MappingVersion})
	}
	return out, nil
}
