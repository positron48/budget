//go:build !ignore
// +build !ignore

package grpcadapter

import (
    "context"
    "time"

    budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
    "google.golang.org/protobuf/types/known/timestamppb"
)

type FxRepo interface {
    GetRateAsOf(ctx context.Context, from, to string, asOf time.Time) (rateDecimal string, provider string, err error)
    UpsertRate(ctx context.Context, from, to, rateDecimal string, asOf time.Time, provider string) (struct{
        From, To, Rate string
        AsOf           time.Time
        Provider       string
    }, error)
    BatchGetRates(ctx context.Context, fromCurrencies []string, to string, asOf time.Time) ([]struct{
        From, To, Rate string
        AsOf           time.Time
        Provider       string
    }, error)
}

type FxServer struct {
    budgetv1.UnimplementedFxServiceServer
    repo FxRepo
}

func NewFxServer(repo FxRepo) *FxServer { return &FxServer{repo: repo} }

func (s *FxServer) GetRate(ctx context.Context, req *budgetv1.GetRateRequest) (*budgetv1.GetRateResponse, error) {
    asOf := time.Now()
    if req.GetAsOf() != nil {
        asOf = req.GetAsOf().AsTime()
    }
    rate, provider, err := s.repo.GetRateAsOf(ctx, req.GetFromCurrencyCode(), req.GetToCurrencyCode(), asOf)
    if err != nil { return nil, mapError(err) }
    return &budgetv1.GetRateResponse{Rate: &budgetv1.FxRate{FromCurrencyCode: req.GetFromCurrencyCode(), ToCurrencyCode: req.GetToCurrencyCode(), RateDecimal: rate, AsOf: timestamppb.New(asOf), Provider: provider}}, nil
}

func (s *FxServer) UpsertRate(ctx context.Context, req *budgetv1.UpsertRateRequest) (*budgetv1.UpsertRateResponse, error) {
    r := req.GetRate()
    asOf := time.Now()
    if r.GetAsOf() != nil {
        asOf = r.GetAsOf().AsTime()
    }
    row, err := s.repo.UpsertRate(ctx, r.GetFromCurrencyCode(), r.GetToCurrencyCode(), r.GetRateDecimal(), asOf, r.GetProvider())
    if err != nil { return nil, mapError(err) }
    return &budgetv1.UpsertRateResponse{Rate: &budgetv1.FxRate{FromCurrencyCode: row.From, ToCurrencyCode: row.To, RateDecimal: row.Rate, AsOf: timestamppb.New(row.AsOf), Provider: row.Provider}}, nil
}

func (s *FxServer) BatchGetRates(ctx context.Context, req *budgetv1.BatchGetRatesRequest) (*budgetv1.BatchGetRatesResponse, error) {
    asOf := time.Now()
    if req.GetAsOf() != nil {
        asOf = req.GetAsOf().AsTime()
    }
    rows, err := s.repo.BatchGetRates(ctx, req.GetFromCurrencyCodes(), req.GetToCurrencyCode(), asOf)
    if err != nil { return nil, mapError(err) }
    out := make([]*budgetv1.FxRate, 0, len(rows))
    for _, r := range rows {
        out = append(out, &budgetv1.FxRate{FromCurrencyCode: r.From, ToCurrencyCode: r.To, RateDecimal: r.Rate, AsOf: timestamppb.New(r.AsOf), Provider: r.Provider})
    }
    return &budgetv1.BatchGetRatesResponse{Rates: out}, nil
}


