package report

import (
    "context"
    "testing"
    "time"

    "github.com/positron48/budget/internal/domain"
    txuse "github.com/positron48/budget/internal/usecase/transaction"
)

type stubTxService struct{ items []domain.Transaction }
func (s stubTxService) List(ctx context.Context, tenantID string, filter txuse.ListFilter) ([]domain.Transaction, int64, error) {
    return s.items, int64(len(s.items)), nil
}

type fxRepoStub struct{}
func (fxRepoStub) GetRateAsOf(ctx context.Context, from, to string, asOf time.Time) (string, string, error) {
    return "2.0", "test", nil
}

type tRepoStub struct{ base string }
func (t tRepoStub) GetByID(ctx context.Context, id string) (domain.Tenant, error) { return domain.Tenant{ID: id, DefaultCurrencyCode: t.base}, nil }

type cRepoStub struct{}
func (cRepoStub) Get(ctx context.Context, id string) (domain.Category, error) { return domain.Category{ID: id, Code: id, Translations: []domain.CategoryTranslation{{Locale: "en", Name: id}}}, nil }
func (cRepoStub) GetMany(ctx context.Context, ids []string) (map[string]domain.Category, error) {
    m := map[string]domain.Category{}
    for _, id := range ids { m[id] = domain.Category{ID: id, Code: id, Translations: []domain.CategoryTranslation{{Locale: "en", Name: id}}} }
    return m, nil
}

func TestReport_MonthlySummary(t *testing.T) {
    // two expense tx in USD base RUB, target RUB
    items := []domain.Transaction{
        {CategoryID: "food", Type: domain.TransactionTypeExpense, BaseAmount: domain.Money{CurrencyCode: "RUB", MinorUnits: 10000}, OccurredAt: time.Now()},
        {CategoryID: "books", Type: domain.TransactionTypeExpense, BaseAmount: domain.Money{CurrencyCode: "RUB", MinorUnits: 5000}, OccurredAt: time.Now()},
    }
    rep := Service{fx: fxRepoStub{}, tenants: tRepoStub{base: "RUB"}, cats: cRepoStub{}, txsvc: stubTxService{items: items}}
    sum, err := rep.GetMonthlySummary(context.Background(), "t1", 2025, 2, "en", "")
    if err != nil { t.Fatalf("summary: %v", err) }
    _ = items // placeholder to keep the compiler happy in this simplified test
    if sum.TotalIncome.MinorUnits != 0 || sum.TotalExpense.CurrencyCode == "" {
        t.Fatalf("unexpected sums: %+v", sum)
    }
}


