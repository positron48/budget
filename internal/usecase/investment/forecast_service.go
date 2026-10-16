package investment

import (
	"context"
	"strings"
	"unicode/utf8"

	d "github.com/positron48/budget/internal/domain"
)

type forecastRepo interface {
	Forecast(context.Context, string, string, ForecastQuery) (ForecastReport, error)
	ForecastCatalog(context.Context, string, string) ([]ForecastEvent, []ForecastLink, error)
	SaveForecastEvent(context.Context, string, string, ForecastEvent, int64) (ForecastEvent, error)
	SaveForecastLink(context.Context, string, string, ForecastLink, int64) (ForecastLink, error)
}

func NormalizeForecastLink(link ForecastLink) (ForecastLink, error) {
	if !validID(link.EventID) || !validID(link.LedgerID) || !validID(link.OperationID) || link.EventVersion < 1 || link.OperationVersion < 1 || link.Version < 0 || strings.TrimSpace(link.Reason) == "" || len(link.Reason) > 4000 || !utf8.ValidString(link.Reason) || strings.ContainsRune(link.Reason, 0) {
		return link, d.ErrInvestmentInvalid
	}
	return link, nil
}

func (s *Service) Forecast(ctx context.Context, t, u string, q ForecastQuery) (ForecastReport, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return ForecastReport{}, e
	}
	q, e := NormalizeForecastQuery(q)
	if e != nil {
		return ForecastReport{}, e
	}
	repo, ok := s.repo.(forecastRepo)
	if !ok {
		return ForecastReport{}, d.ErrInvestmentPrecondition
	}
	return repo.Forecast(ctx, t, u, q)
}

func (s *Service) ForecastCatalog(ctx context.Context, t, u string) ([]ForecastEvent, []ForecastLink, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return nil, nil, e
	}
	repo, ok := s.repo.(forecastRepo)
	if !ok {
		return nil, nil, d.ErrInvestmentPrecondition
	}
	return repo.ForecastCatalog(ctx, t, u)
}

func (s *Service) SaveForecastEvent(ctx context.Context, t, u string, event ForecastEvent, expected int64) (ForecastEvent, error) {
	if event.Origin != "" {
		return ForecastEvent{}, d.ErrInvestmentInvalid
	}
	if e := s.authorize(ctx, t, u, true); e != nil {
		return ForecastEvent{}, e
	}
	event, e := NormalizeForecastEvent(event)
	if e != nil || expected < 0 || expected == 1<<63-1 {
		return ForecastEvent{}, d.ErrInvestmentInvalid
	}
	repo, ok := s.repo.(forecastRepo)
	if !ok {
		return ForecastEvent{}, d.ErrInvestmentPrecondition
	}
	return repo.SaveForecastEvent(ctx, t, u, event, expected)
}

func (s *Service) SaveForecastLink(ctx context.Context, t, u string, link ForecastLink, expected int64) (ForecastLink, error) {
	if e := s.authorize(ctx, t, u, true); e != nil {
		return ForecastLink{}, e
	}
	link, e := NormalizeForecastLink(link)
	if e != nil || expected < 0 || expected == 1<<63-1 {
		return ForecastLink{}, d.ErrInvestmentInvalid
	}
	repo, ok := s.repo.(forecastRepo)
	if !ok {
		return ForecastLink{}, d.ErrInvestmentPrecondition
	}
	return repo.SaveForecastLink(ctx, t, u, link, expected)
}
