package transaction

import (
	"context"
	"testing"
	"time"

	"github.com/positron48/budget/internal/domain"
)

type stubFxRepo struct {
	rate     string
	provider string
	err      error
}

func (s stubFxRepo) GetRateAsOf(ctx context.Context, from, to string, asOf time.Time) (string, string, error) {
	return s.rate, s.provider, s.err
}

type stubTenantRepo struct{ defCcy string }

func (s stubTenantRepo) GetByID(ctx context.Context, id string) (domain.Tenant, error) {
	return domain.Tenant{ID: id, DefaultCurrencyCode: s.defCcy}, nil
}

type stubCategoryRepo struct{}

func (stubCategoryRepo) Get(ctx context.Context, id string) (domain.Category, error) {
	return domain.Category{ID: id, TenantID: "t1", Kind: domain.CategoryKindExpense}, nil
}

type noopTxRepo struct{}

func (noopTxRepo) Create(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	return tx, nil
}

func (noopTxRepo) Update(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	return tx, nil
}
func (noopTxRepo) Delete(ctx context.Context, id string) error { return nil }
func (noopTxRepo) Get(ctx context.Context, id string) (domain.Transaction, error) {
	return domain.Transaction{ID: id}, nil
}

func (noopTxRepo) List(ctx context.Context, tenantID string, filter ListFilter) ([]domain.Transaction, int64, error) {
	return nil, 0, nil
}

func TestComputeBaseAmount_SameCurrency(t *testing.T) {
	svc := NewService(noopTxRepo{}, stubFxRepo{}, stubTenantRepo{defCcy: "USD"}, stubCategoryRepo{})
	amount := domain.Money{CurrencyCode: "USD", MinorUnits: 12345}
	base, fx, err := svc.ComputeBaseAmount(context.Background(), "t1", amount, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fx != nil {
		t.Fatalf("expected nil fx when currency matches, got %#v", fx)
	}
	if base.CurrencyCode != "USD" || base.MinorUnits != amount.MinorUnits {
		t.Fatalf("unexpected base: %#v", base)
	}
}

func TestComputeBaseAmount_DifferentCurrency_RateFound(t *testing.T) {
	svc := NewService(noopTxRepo{}, stubFxRepo{rate: "2.5000", provider: "test"}, stubTenantRepo{defCcy: "EUR"}, stubCategoryRepo{})
	amount := domain.Money{CurrencyCode: "USD", MinorUnits: 100}
	base, fx, err := svc.ComputeBaseAmount(context.Background(), "t1", amount, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fx == nil || fx.RateDecimal != "2.5000" || fx.ToCurrency != "EUR" || fx.FromCurrency != "USD" {
		t.Fatalf("unexpected fx: %#v", fx)
	}
	if base.CurrencyCode != "EUR" || base.MinorUnits != amount.MinorUnits {
		t.Fatalf("unexpected base: %#v", base)
	}
}

func TestComputeBaseAmount_RateNotFound(t *testing.T) {
	svc := NewService(noopTxRepo{}, stubFxRepo{err: ErrFxRateNotFound}, stubTenantRepo{defCcy: "EUR"}, stubCategoryRepo{})
	_, _, err := svc.ComputeBaseAmount(context.Background(), "t1", domain.Money{CurrencyCode: "USD", MinorUnits: 100}, time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
}
