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

func (noopTxRepo) Create(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) { tx.ID = "tx1"; return tx, nil }

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

func TestService_CreateForUser_ValidationsAndCompute(t *testing.T) {
    svc := NewService(noopTxRepo{}, stubFxRepo{rate: "1.0000", provider: "test"}, stubTenantRepo{defCcy: "RUB"}, stubCategoryRepo{})
    tx, err := svc.CreateForUser(context.Background(), "t1", "u1", domain.TransactionTypeExpense, "cat1", domain.Money{CurrencyCode: "USD", MinorUnits: 123}, time.Now(), "")
    if err != nil || tx.ID == "" || tx.BaseAmount.CurrencyCode != "RUB" { t.Fatalf("create: %v %#v", err, tx) }
}

type captureTxRepo struct{ lastDeleted string; lastListedTenant string; lastUpdated domain.Transaction }
func (c *captureTxRepo) Create(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) { return tx, nil }
func (c *captureTxRepo) Update(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) { c.lastUpdated = tx; return tx, nil }
func (c *captureTxRepo) Delete(ctx context.Context, id string) error { c.lastDeleted = id; return nil }
func (c *captureTxRepo) Get(ctx context.Context, id string) (domain.Transaction, error) { return domain.Transaction{ID: id}, nil }
func (c *captureTxRepo) List(ctx context.Context, tenantID string, filter ListFilter) ([]domain.Transaction, int64, error) { c.lastListedTenant = tenantID; return []domain.Transaction{}, 0, nil }

func TestService_Update_RecomputesBaseAndFx(t *testing.T) {
    cap := &captureTxRepo{}
    svc := NewService(cap, stubFxRepo{rate: "2.0000", provider: "prov"}, stubTenantRepo{defCcy: "EUR"}, stubCategoryRepo{})
    tx := domain.Transaction{ID: "tx1", TenantID: "t1", Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 250}, OccurredAt: time.Now(), Type: domain.TransactionTypeExpense, CategoryID: "cat1"}
    out, err := svc.Update(context.Background(), tx)
    if err != nil { t.Fatalf("update: %v", err) }
    if out.BaseAmount.CurrencyCode != "EUR" || out.Fx == nil || out.Fx.RateDecimal != "2.0000" { t.Fatalf("base/fx not recomputed: %#v", out) }
}

type incomeCatRepo struct{}
func (incomeCatRepo) Get(ctx context.Context, id string) (domain.Category, error) { return domain.Category{ID: id, TenantID: "t1", Kind: domain.CategoryKindIncome}, nil }

func TestService_Update_TypeMismatch(t *testing.T) {
    svc := NewService(noopTxRepo{}, stubFxRepo{}, stubTenantRepo{defCcy: "EUR"}, incomeCatRepo{})
    _, err := svc.Update(context.Background(), domain.Transaction{ID: "tx1", TenantID: "t1", Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 1}, OccurredAt: time.Now(), Type: domain.TransactionTypeExpense, CategoryID: "cat1"})
    if err == nil { t.Fatal("expected type mismatch error") }
}

func TestService_DeleteAndList_Delegation(t *testing.T) {
    cap := &captureTxRepo{}
    svc := NewService(cap, stubFxRepo{}, stubTenantRepo{defCcy: "EUR"}, stubCategoryRepo{})
    if err := svc.Delete(context.Background(), "tx1"); err != nil || cap.lastDeleted != "tx1" { t.Fatalf("delete: %v", err) }
    _, _, err := svc.List(context.Background(), "t1", ListFilter{Page: 1, PageSize: 10})
    if err != nil || cap.lastListedTenant != "t1" { t.Fatalf("list: %v", err) }
}

func TestService_Update_SameCurrencyNoFx(t *testing.T) {
    cap := &captureTxRepo{}
    svc := NewService(cap, stubFxRepo{}, stubTenantRepo{defCcy: "USD"}, stubCategoryRepo{})
    tx := domain.Transaction{ID: "tx2", TenantID: "t1", Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 1000}, OccurredAt: time.Now(), Type: domain.TransactionTypeExpense, CategoryID: "cat1"}
    out, err := svc.Update(context.Background(), tx)
    if err != nil { t.Fatalf("update: %v", err) }
    if out.Fx != nil || out.BaseAmount.CurrencyCode != "USD" || out.BaseAmount.MinorUnits != tx.Amount.MinorUnits {
        t.Fatalf("expected no fx and same base amount: %#v", out)
    }
}

type otherTenantCatRepo struct{}
func (otherTenantCatRepo) Get(ctx context.Context, id string) (domain.Category, error) { return domain.Category{ID: id, TenantID: "other", Kind: domain.CategoryKindExpense}, nil }

func TestService_Update_InvalidCategoryTenant(t *testing.T) {
    svc := NewService(noopTxRepo{}, stubFxRepo{}, stubTenantRepo{defCcy: "USD"}, otherTenantCatRepo{})
    _, err := svc.Update(context.Background(), domain.Transaction{ID: "tx", TenantID: "t1", Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 1}, OccurredAt: time.Now(), Type: domain.TransactionTypeExpense, CategoryID: "cat1"})
    if err == nil { t.Fatal("expected invalid category/tenant error") }
}

type fxErrRepo struct{}
func (fxErrRepo) GetRateAsOf(ctx context.Context, from, to string, asOf time.Time) (string, string, error) { return "", "", ErrFxRateNotFound }

func TestService_Update_RateNotFound(t *testing.T) {
    svc := NewService(noopTxRepo{}, fxErrRepo{}, stubTenantRepo{defCcy: "EUR"}, stubCategoryRepo{})
    _, err := svc.Update(context.Background(), domain.Transaction{ID: "tx", TenantID: "t1", Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 1}, OccurredAt: time.Now(), Type: domain.TransactionTypeExpense, CategoryID: "cat1"})
    if err == nil { t.Fatal("expected fx rate not found error") }
}

type incomeCatRepo2 struct{}
func (incomeCatRepo2) Get(ctx context.Context, id string) (domain.Category, error) { return domain.Category{ID: id, TenantID: "t1", Kind: domain.CategoryKindIncome}, nil }

func TestService_CreateForUser_TypeMismatch(t *testing.T) {
    svc := NewService(noopTxRepo{}, stubFxRepo{}, stubTenantRepo{defCcy: "EUR"}, incomeCatRepo2{})
    _, err := svc.CreateForUser(context.Background(), "t1", "u1", domain.TransactionTypeExpense, "cat1", domain.Money{CurrencyCode: "EUR", MinorUnits: 100}, time.Now(), "")
    if err == nil { t.Fatal("expected type mismatch") }
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
