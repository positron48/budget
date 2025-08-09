package transaction

import (
	"context"
	"errors"
	"time"

	"github.com/positron48/budget/internal/domain"
)

var ErrFxRateNotFound = errors.New("fx rate not found")

type TxRepo interface {
	Create(ctx context.Context, tx domain.Transaction) (domain.Transaction, error)
	Update(ctx context.Context, tx domain.Transaction) (domain.Transaction, error)
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (domain.Transaction, error)
	List(ctx context.Context, tenantID string, filter ListFilter) ([]domain.Transaction, int64, error)
}

type FxRepo interface {
	GetRateAsOf(ctx context.Context, from, to string, asOf time.Time) (rateDecimal string, provider string, err error)
}

type TenantRepo interface {
	GetByID(ctx context.Context, id string) (domain.Tenant, error)
}

type ListFilter struct {
	From          *time.Time
	To            *time.Time
	CategoryIDs   []string
	Type          *domain.TransactionType
	MinMinorUnits *int64
	MaxMinorUnits *int64
	CurrencyCode  *string
	Search        *string
	Page          int
	PageSize      int
}

type Service struct {
	txs     TxRepo
	fx      FxRepo
	tenants TenantRepo
}

func NewService(txs TxRepo, fx FxRepo, tenants TenantRepo) *Service {
	return &Service{txs: txs, fx: fx, tenants: tenants}
}

// ComputeBaseAmount converts original amount to tenant base currency on occurred date
func (s *Service) ComputeBaseAmount(ctx context.Context, tenantID string, amount domain.Money, occurredAt time.Time) (base domain.Money, fx *domain.FxInfo, err error) {
	tenant, err := s.tenants.GetByID(ctx, tenantID)
	if err != nil {
		return domain.Money{}, nil, err
	}
	if amount.CurrencyCode == tenant.DefaultCurrencyCode {
		return amount, nil, nil
	}
	rateDec, provider, err := s.fx.GetRateAsOf(ctx, amount.CurrencyCode, tenant.DefaultCurrencyCode, occurredAt)
	if err != nil {
		return domain.Money{}, nil, ErrFxRateNotFound
	}
	// Note: вычисление по десятичной строке будет делаться в репозитории с NUMERIC, здесь заполним только FxInfo
	fx = &domain.FxInfo{FromCurrency: amount.CurrencyCode, ToCurrency: tenant.DefaultCurrencyCode, RateDecimal: rateDec, AsOf: occurredAt, Provider: provider}
	// base.MajorUnits вычислит repo при записи; в usecase хранится исходная модель
	return domain.Money{CurrencyCode: tenant.DefaultCurrencyCode, MinorUnits: amount.MinorUnits}, fx, nil
}
