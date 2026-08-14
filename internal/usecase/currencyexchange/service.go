package currencyexchange

import (
	"context"
	"errors"
	"strings"

	"github.com/positron48/budget/internal/domain"
)

var (
	ErrInvalidAmount   = errors.New("exchange amounts must be positive")
	ErrInvalidCurrency = errors.New("exchange currencies must be different ISO 4217 codes")
)

type Repo interface {
	Create(ctx context.Context, exchange domain.CurrencyExchange) (domain.CurrencyExchange, error)
	List(ctx context.Context, tenantID string, page, pageSize int) ([]domain.CurrencyExchange, int64, error)
	Delete(ctx context.Context, tenantID, id string) error
}

type Service struct {
	repo Repo
}

func NewService(repo Repo) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, exchange domain.CurrencyExchange) (domain.CurrencyExchange, error) {
	if exchange.FromAmount.MinorUnits <= 0 || exchange.ToAmount.MinorUnits <= 0 {
		return domain.CurrencyExchange{}, ErrInvalidAmount
	}
	exchange.FromAmount.CurrencyCode = strings.ToUpper(strings.TrimSpace(exchange.FromAmount.CurrencyCode))
	exchange.ToAmount.CurrencyCode = strings.ToUpper(strings.TrimSpace(exchange.ToAmount.CurrencyCode))
	if len(exchange.FromAmount.CurrencyCode) != 3 || len(exchange.ToAmount.CurrencyCode) != 3 || exchange.FromAmount.CurrencyCode == exchange.ToAmount.CurrencyCode {
		return domain.CurrencyExchange{}, ErrInvalidCurrency
	}
	return s.repo.Create(ctx, exchange)
}

func (s *Service) List(ctx context.Context, tenantID string, page, pageSize int) ([]domain.CurrencyExchange, int64, error) {
	return s.repo.List(ctx, tenantID, page, pageSize)
}

func (s *Service) Delete(ctx context.Context, tenantID, id string) error {
	return s.repo.Delete(ctx, tenantID, id)
}
