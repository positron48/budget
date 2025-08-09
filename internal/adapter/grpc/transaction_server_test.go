package grpcadapter

import (
    "context"
    "errors"
    "testing"
    "time"

    budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
    "github.com/positron48/budget/internal/domain"
    "github.com/positron48/budget/internal/pkg/ctxutil"
    txuse "github.com/positron48/budget/internal/usecase/transaction"
    "google.golang.org/protobuf/types/known/fieldmaskpb"
    "google.golang.org/protobuf/types/known/timestamppb"
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

type txSvcBaseErr struct{}
func (txSvcBaseErr) ComputeBaseAmount(ctx context.Context, tenantID string, amount domain.Money, occurredAt time.Time) (domain.Money, *domain.FxInfo, error) { return domain.Money{}, nil, errors.New("fx err") }
func (txSvcBaseErr) Create(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) { return domain.Transaction{}, nil }
func (txSvcBaseErr) Update(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) { return domain.Transaction{}, nil }
func (txSvcBaseErr) Delete(ctx context.Context, id string) error { return nil }
func (txSvcBaseErr) Get(ctx context.Context, id string) (domain.Transaction, error) { return domain.Transaction{ID: id, TenantID: "t1", Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 100}, OccurredAt: time.Now()}, nil }
func (txSvcBaseErr) List(ctx context.Context, tenantID string, filter txuse.ListFilter) ([]domain.Transaction, int64, error) { return nil, 0, nil }
func (txSvcBaseErr) CreateForUser(ctx context.Context, tenantID, userID string, txType domain.TransactionType, categoryID string, amount domain.Money, occurredAt time.Time, comment string) (domain.Transaction, error) { return domain.Transaction{}, nil }

func TestTransactionServer_MapError(t *testing.T) {
    stubErr := txSvcStub{err: errors.New("boom")}
    srv := NewTransactionServer(stubErr)
    _, err := srv.UpdateTransaction(context.Background(), &budgetv1.UpdateTransactionRequest{Id: "nope", Transaction: &budgetv1.Transaction{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"amount"}}})
    if err == nil { t.Fatal("expected error") }
    if _, err := srv.DeleteTransaction(context.Background(), &budgetv1.DeleteTransactionRequest{Id: "x"}); err == nil { t.Fatal("expected error") }
    if _, err := srv.GetTransaction(context.Background(), &budgetv1.GetTransactionRequest{Id: "x"}); err == nil { t.Fatal("expected error") }
    if _, err := srv.ListTransactions(context.Background(), &budgetv1.ListTransactionsRequest{}); err == nil { t.Fatal("expected error") }
}

func TestTransactionServer_Create_InvalidInput(t *testing.T) {
    srv := NewTransactionServer(txSvcStub{})
    // nil amount
    _, err := srv.CreateTransaction(context.Background(), &budgetv1.CreateTransactionRequest{CategoryId: "c1"})
    if err == nil { t.Fatal("expected invalid argument") }
}

func TestTransactionServer_Create_Success(t *testing.T) {
    stub := txSvcStub{tx: domain.Transaction{ID: "tx1"}}
    srv := NewTransactionServer(stub)
    req := &budgetv1.CreateTransactionRequest{CategoryId: "c1", Amount: &budgetv1.Money{CurrencyCode: "USD", MinorUnits: 100}}
    out, err := srv.CreateTransaction(context.Background(), req)
    if err != nil || out.GetTransaction().GetId() != "tx1" { t.Fatalf("unexpected: %v %#v", err, out) }
}

func TestTransactionServer_Create_MapError(t *testing.T) {
    srv := NewTransactionServer(txSvcStub{err: errors.New("boom")})
    req := &budgetv1.CreateTransactionRequest{CategoryId: "c1", Amount: &budgetv1.Money{CurrencyCode: "USD", MinorUnits: 100}}
    if _, err := srv.CreateTransaction(context.Background(), req); err == nil { t.Fatal("expected error") }
}

func TestTransactionServer_Update_ComputeBaseError(t *testing.T) {
    srv := NewTransactionServer(txSvcBaseErr{})
    patch := &budgetv1.Transaction{Amount: &budgetv1.Money{CurrencyCode: "USD", MinorUnits: 200}}
    mask := &fieldmaskpb.FieldMask{Paths: []string{"amount"}}
    if _, err := srv.UpdateTransaction(context.Background(), &budgetv1.UpdateTransactionRequest{Id: "tx1", Transaction: patch, UpdateMask: mask}); err == nil {
        t.Fatal("expected compute base error")
    }
}

func TestTransactionServer_Update_WithMask(t *testing.T) {
    existing := domain.Transaction{ID: "tx1", TenantID: "t1", Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 100}, OccurredAt: time.Now()}
    stub := txSvcStub{tx: existing}
    srv := NewTransactionServer(stub)
    patch := &budgetv1.Transaction{Amount: &budgetv1.Money{CurrencyCode: "USD", MinorUnits: 200}}
    mask := &fieldmaskpb.FieldMask{Paths: []string{"amount"}}
    _, err := srv.UpdateTransaction(context.Background(), &budgetv1.UpdateTransactionRequest{Id: "tx1", Transaction: patch, UpdateMask: mask})
    if err != nil { t.Fatalf("update: %v", err) }
}

// echo service to verify fx propagated via toProtoTx
type txSvcEcho struct{ cur domain.Transaction }
func (txSvcEcho) ComputeBaseAmount(ctx context.Context, tenantID string, amount domain.Money, occurredAt time.Time) (domain.Money, *domain.FxInfo, error) {
    fx := &domain.FxInfo{FromCurrency: amount.CurrencyCode, ToCurrency: "EUR", RateDecimal: "2.0000", AsOf: occurredAt, Provider: "prov"}
    return domain.Money{CurrencyCode: "EUR", MinorUnits: amount.MinorUnits}, fx, nil
}
func (txSvcEcho) Create(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) { return tx, nil }
func (txSvcEcho) Update(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) { return tx, nil }
func (txSvcEcho) Delete(ctx context.Context, id string) error { return nil }
func (e txSvcEcho) Get(ctx context.Context, id string) (domain.Transaction, error) { return e.cur, nil }
func (txSvcEcho) List(ctx context.Context, tenantID string, filter txuse.ListFilter) ([]domain.Transaction, int64, error) { return nil, 0, nil }
func (txSvcEcho) CreateForUser(ctx context.Context, tenantID, userID string, txType domain.TransactionType, categoryID string, amount domain.Money, occurredAt time.Time, comment string) (domain.Transaction, error) { return domain.Transaction{ID: "tx"}, nil }

func TestTransactionServer_Update_FxIncluded(t *testing.T) {
    cur := domain.Transaction{ID: "tx1", TenantID: "t1", Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 150}, OccurredAt: time.Now()}
    srv := NewTransactionServer(txSvcEcho{cur: cur})
    patch := &budgetv1.Transaction{Amount: &budgetv1.Money{CurrencyCode: "USD", MinorUnits: 300}}
    mask := &fieldmaskpb.FieldMask{Paths: []string{"amount"}}
    out, err := srv.UpdateTransaction(context.Background(), &budgetv1.UpdateTransactionRequest{Id: "tx1", Transaction: patch, UpdateMask: mask})
    if err != nil { t.Fatalf("update fx: %v", err) }
    if out.GetTransaction().GetBaseAmount().GetCurrencyCode() != "EUR" || out.GetTransaction().GetFx() == nil || out.GetTransaction().GetFx().GetRateDecimal() != "2.0000" {
        t.Fatalf("fx not propagated: %#v", out.GetTransaction())
    }
}

func TestTransactionServer_Delete_Get_List(t *testing.T) {
    items := []domain.Transaction{{ID: "tx1"}, {ID: "tx2"}}
    stub := txSvcStub{tx: items[0], items: items, total: 2}
    srv := NewTransactionServer(stub)
    if _, err := srv.DeleteTransaction(context.Background(), &budgetv1.DeleteTransactionRequest{Id: "tx1"}); err != nil { t.Fatalf("delete: %v", err) }
    g, err := srv.GetTransaction(context.Background(), &budgetv1.GetTransactionRequest{Id: "tx1"})
    if err != nil || g.GetTransaction().GetId() != "tx1" { t.Fatalf("get: %v %#v", err, g) }
    lst, err := srv.ListTransactions(context.Background(), &budgetv1.ListTransactionsRequest{Page: &budgetv1.PageRequest{Page: 1, PageSize: 2}})
    if err != nil || len(lst.GetTransactions()) != 2 || lst.GetPage().GetTotalItems() != 2 { t.Fatalf("list: %v %#v", err, lst) }
}

func TestTransactionServer_List_DefaultPaging(t *testing.T) {
    // pageSize=0 -> default 50; totalPages should be 1 when total < default page size
    items := []domain.Transaction{{ID: "a"}}
    srv := NewTransactionServer(txSvcStub{items: items, total: int64(len(items))})
    out, err := srv.ListTransactions(context.Background(), &budgetv1.ListTransactionsRequest{})
    if err != nil { t.Fatalf("list: %v", err) }
    if out.GetPage().GetPageSize() != 50 || out.GetPage().GetTotalPages() != 1 { t.Fatalf("unexpected paging: %#v", out.GetPage()) }
}

func TestTransactionServer_List_WithFilters(t *testing.T) {
    srv := NewTransactionServer(txSvcStub{})
    now := time.Now()
    req := &budgetv1.ListTransactionsRequest{
        DateRange: &budgetv1.DateRange{From: timestamppb.New(now.Add(-24 * time.Hour)), To: timestamppb.New(now)},
        CategoryIds: []string{"c1", "c2"},
        Type: budgetv1.TransactionType_TRANSACTION_TYPE_INCOME,
        MinMinorUnits: 10,
        MaxMinorUnits: 1000,
        CurrencyCode: "USD",
        Search: "book",
        Page: &budgetv1.PageRequest{Page: 2, PageSize: 10},
    }
    ctx := ctxutil.WithTenantID(context.Background(), "t1")
    if _, err := srv.ListTransactions(ctx, req); err != nil { t.Fatalf("list with filters: %v", err) }
}

// Further server tests can be added by refactoring server to accept an interface in constructor


