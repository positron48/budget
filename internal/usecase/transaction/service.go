package transaction

import (
	"context"
	"errors"
	"time"

	"github.com/positron48/budget/internal/domain"
)

var (
	ErrFxRateNotFound  = errors.New("fx rate not found")
	ErrInvalidCategory = errors.New("invalid category")
	ErrTypeMismatch    = errors.New("transaction type does not match category kind")
)

type TxRepo interface {
	Create(ctx context.Context, tx domain.Transaction) (domain.Transaction, error)
	Update(ctx context.Context, tx domain.Transaction) (domain.Transaction, error)
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (domain.Transaction, error)
	List(ctx context.Context, tenantID string, filter ListFilter) ([]domain.Transaction, int64, error)
	Totals(ctx context.Context, tenantID string, filter ListFilter) (totalIncomeMinor int64, totalExpenseMinor int64, baseCurrency string, err error)
}

type FxRepo interface {
	GetRateAsOf(ctx context.Context, from, to string, asOf time.Time) (rateDecimal string, provider string, err error)
}

type TenantRepo interface {
	GetByID(ctx context.Context, id string) (domain.Tenant, error)
}

type CategoryRepo interface {
	Get(ctx context.Context, id string) (domain.Category, error)
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
	Sort          string // e.g. "occurred_at desc", "amount_numeric asc", "comment asc"
}

type Service struct {
	txs     TxRepo
	fx      FxRepo
	tenants TenantRepo
	cats    CategoryRepo
}

func NewService(txs TxRepo, fx FxRepo, tenants TenantRepo, cats CategoryRepo) *Service {
	return &Service{txs: txs, fx: fx, tenants: tenants, cats: cats}
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

// CRUD and query operations delegate to repository layer
func (s *Service) Create(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	return s.txs.Create(ctx, tx)
}

func (s *Service) Update(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	// validate category
	cat, err := s.cats.Get(ctx, tx.CategoryID)
	if err != nil || cat.TenantID != tx.TenantID {
		return domain.Transaction{}, ErrInvalidCategory
	}
	if (cat.Kind == domain.CategoryKindIncome && tx.Type != domain.TransactionTypeIncome) || (cat.Kind == domain.CategoryKindExpense && tx.Type != domain.TransactionTypeExpense) {
		return domain.Transaction{}, ErrTypeMismatch
	}
	// recompute base (always safe)
	base, fx, err := s.ComputeBaseAmount(ctx, tx.TenantID, tx.Amount, tx.OccurredAt)
	if err != nil {
		return domain.Transaction{}, err
	}
	tx.BaseAmount = base
	tx.Fx = fx
	return s.txs.Update(ctx, tx)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.txs.Delete(ctx, id)
}

func (s *Service) Get(ctx context.Context, id string) (domain.Transaction, error) {
	return s.txs.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, tenantID string, filter ListFilter) ([]domain.Transaction, int64, error) {
	return s.txs.List(ctx, tenantID, filter)
}

// Totals returns income/expense totals in tenant base currency according to filter (ignoring pagination)
func (s *Service) Totals(ctx context.Context, tenantID string, filter ListFilter) (domain.Money, domain.Money, error) {
	tenant, err := s.tenants.GetByID(ctx, tenantID)
	if err != nil {
		return domain.Money{}, domain.Money{}, err
	}
	inc, exp, baseCurrency, err := s.txs.Totals(ctx, tenantID, filter)
	if err != nil {
		return domain.Money{}, domain.Money{}, err
	}
	// fall back to tenant base currency if repo did not return it
	if baseCurrency == "" {
		baseCurrency = tenant.DefaultCurrencyCode
	}
	return domain.Money{CurrencyCode: baseCurrency, MinorUnits: inc}, domain.Money{CurrencyCode: baseCurrency, MinorUnits: exp}, nil
}

// High-level API used by controller (business rules live here)
func (s *Service) CreateForUser(ctx context.Context, tenantID, userID string, txType domain.TransactionType, categoryID string, amount domain.Money, occurredAt time.Time, comment string) (domain.Transaction, error) {
	// validate category
	cat, err := s.cats.Get(ctx, categoryID)
	if err != nil || cat.TenantID != tenantID {
		return domain.Transaction{}, ErrInvalidCategory
	}
	if (cat.Kind == domain.CategoryKindIncome && txType != domain.TransactionTypeIncome) || (cat.Kind == domain.CategoryKindExpense && txType != domain.TransactionTypeExpense) {
		return domain.Transaction{}, ErrTypeMismatch
	}
	base, fx, err := s.ComputeBaseAmount(ctx, tenantID, amount, occurredAt)
	if err != nil {
		return domain.Transaction{}, err
	}
	tx := domain.Transaction{
		TenantID:   tenantID,
		UserID:     userID,
		CategoryID: categoryID,
		Type:       txType,
		Amount:     amount,
		BaseAmount: base,
		Fx:         fx,
		OccurredAt: occurredAt,
		Comment:    comment,
	}
	return s.txs.Create(ctx, tx)
}
