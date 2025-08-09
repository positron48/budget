package grpcadapter

import (
    "context"
    "testing"
    "time"

    budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
    "github.com/positron48/budget/internal/domain"
    txuse "github.com/positron48/budget/internal/usecase/transaction"
)

type txSvcStub struct{ tx domain.Transaction; err error; items []domain.Transaction; total int64 }
func (s txSvcStub) ComputeBaseAmount(ctx context.Context, tenantID string, amount domain.Money, occurredAt time.Time) (domain.Money, *domain.FxInfo, error) {
    return amount, nil, nil
}
func (s txSvcStub) Create(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) { return s.tx, s.err }
func (s txSvcStub) Update(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) { return s.tx, s.err }
func (s txSvcStub) Delete(ctx context.Context, id string) error { return s.err }
func (s txSvcStub) Get(ctx context.Context, id string) (domain.Transaction, error) { return s.tx, s.err }
func (s txSvcStub) List(ctx context.Context, tenantID string, filter txuse.ListFilter) ([]domain.Transaction, int64, error) { return s.items, s.total, s.err }
func (s txSvcStub) CreateForUser(ctx context.Context, tenantID, userID string, txType domain.TransactionType, categoryID string, amount domain.Money, occurredAt time.Time, comment string) (domain.Transaction, error) {
    return s.tx, s.err
}

func TestTransactionServer_Create_InvalidInput(t *testing.T) {
    srv := NewTransactionServer((*txuse.Service)(nil))
    // nil amount
    _, err := srv.CreateTransaction(context.Background(), &budgetv1.CreateTransactionRequest{CategoryId: "c1"})
    if err == nil { t.Fatal("expected invalid argument") }
}

// Further server tests can be added by refactoring server to accept an interface in constructor


