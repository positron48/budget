//go:build !ignore
// +build !ignore

package grpcadapter

import (
    "context"
    "time"

    budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
    "github.com/positron48/budget/internal/domain"
    "github.com/positron48/budget/internal/pkg/ctxutil"
    txusecase "github.com/positron48/budget/internal/usecase/transaction"
    "google.golang.org/protobuf/types/known/fieldmaskpb"
    "google.golang.org/protobuf/types/known/timestamppb"
)

type TransactionServer struct {
    budgetv1.UnimplementedTransactionServiceServer
    svc *txusecase.Service
}

func NewTransactionServer(svc *txusecase.Service) *TransactionServer { return &TransactionServer{svc: svc} }

func (s *TransactionServer) CreateTransaction(ctx context.Context, req *budgetv1.CreateTransactionRequest) (*budgetv1.CreateTransactionResponse, error) {
    tenantID, _ := ctxutil.TenantIDFromContext(ctx)
    userID, _ := ctxutil.UserIDFromContext(ctx)
    amount := domain.Money{CurrencyCode: req.GetAmount().GetCurrencyCode(), MinorUnits: req.GetAmount().GetMinorUnits()}
    occurredAt := time.Now()
    if req.GetOccurredAt() != nil { occurredAt = req.GetOccurredAt().AsTime() }
    created, err := s.svc.CreateForUser(ctx, tenantID, userID, mapTxType(req.GetType()), req.GetCategoryId(), amount, occurredAt, req.GetComment())
    if err != nil { return nil, mapError(err) }
    return &budgetv1.CreateTransactionResponse{Transaction: toProtoTx(created)}, nil
}

func (s *TransactionServer) UpdateTransaction(ctx context.Context, req *budgetv1.UpdateTransactionRequest) (*budgetv1.UpdateTransactionResponse, error) {
    current, err := s.svc.Get(ctx, req.GetId())
    if err != nil { return nil, mapError(err) }
    patch := req.GetTransaction()
    mask := req.GetUpdateMask()
    applyFieldMask(&current, patch, mask)
    if maskAffectsBase(mask) {
        base, fx, err := s.svc.ComputeBaseAmount(ctx, current.TenantID, current.Amount, current.OccurredAt)
        if err != nil {
            return nil, err
        }
        current.BaseAmount = base
        current.Fx = fx
    }
    updated, err := s.svc.Update(ctx, current)
    if err != nil { return nil, mapError(err) }
    return &budgetv1.UpdateTransactionResponse{Transaction: toProtoTx(updated)}, nil
}

func (s *TransactionServer) DeleteTransaction(ctx context.Context, req *budgetv1.DeleteTransactionRequest) (*budgetv1.DeleteTransactionResponse, error) {
    if err := s.svc.Delete(ctx, req.GetId()); err != nil { return nil, mapError(err) }
    return &budgetv1.DeleteTransactionResponse{}, nil
}

func (s *TransactionServer) GetTransaction(ctx context.Context, req *budgetv1.GetTransactionRequest) (*budgetv1.GetTransactionResponse, error) {
    t, err := s.svc.Get(ctx, req.GetId())
    if err != nil { return nil, mapError(err) }
    return &budgetv1.GetTransactionResponse{Transaction: toProtoTx(t)}, nil
}

func (s *TransactionServer) ListTransactions(ctx context.Context, req *budgetv1.ListTransactionsRequest) (*budgetv1.ListTransactionsResponse, error) {
    tenantID, _ := ctxutil.TenantIDFromContext(ctx)
    var f txusecase.ListFilter
    if dr := req.GetDateRange(); dr != nil {
        if dr.GetFrom() != nil {
            t := dr.GetFrom().AsTime()
            f.From = &t
        }
        if dr.GetTo() != nil {
            t := dr.GetTo().AsTime()
            f.To = &t
        }
    }
    f.CategoryIDs = req.GetCategoryIds()
    if req.GetType() != budgetv1.TransactionType_TRANSACTION_TYPE_UNSPECIFIED {
        tt := mapTxType(req.GetType())
        f.Type = &tt
    }
    if req.GetMinMinorUnits() != 0 {
        v := req.GetMinMinorUnits()
        f.MinMinorUnits = &v
    }
    if req.GetMaxMinorUnits() != 0 {
        v := req.GetMaxMinorUnits()
        f.MaxMinorUnits = &v
    }
    if req.GetCurrencyCode() != "" {
        v := req.GetCurrencyCode()
        f.CurrencyCode = &v
    }
    if req.GetSearch() != "" {
        v := req.GetSearch()
        f.Search = &v
    }
    if req.GetPage() != nil {
        f.Page = int(req.GetPage().GetPage())
        f.PageSize = int(req.GetPage().GetPageSize())
    }
    items, total, err := s.svc.List(ctx, tenantID, f)
    if err != nil { return nil, mapError(err) }
    out := make([]*budgetv1.Transaction, 0, len(items))
    for _, it := range items {
        out = append(out, toProtoTx(it))
    }
    pageSize := int32(f.PageSize)
    if pageSize == 0 {
        pageSize = 50
    }
    totalPages := (int32(total) + pageSize - 1) / pageSize
    return &budgetv1.ListTransactionsResponse{Transactions: out, Page: &budgetv1.PageResponse{Page: int32(f.Page), PageSize: pageSize, TotalItems: total, TotalPages: totalPages}}, nil
}

func toProtoTx(t domain.Transaction) *budgetv1.Transaction {
    var fx *budgetv1.FxInfo
    if t.Fx != nil {
        fx = &budgetv1.FxInfo{
            FromCurrencyCode: t.Fx.FromCurrency,
            ToCurrencyCode:   t.Fx.ToCurrency,
            RateDecimal:      t.Fx.RateDecimal,
            AsOf:             timestamppb.New(t.Fx.AsOf),
            Provider:         t.Fx.Provider,
        }
    }
    return &budgetv1.Transaction{
        Id:         t.ID,
        TenantId:   t.TenantID,
        UserId:     t.UserID,
        CategoryId: t.CategoryID,
        Type:       toProtoTxType(t.Type),
        Amount:     &budgetv1.Money{CurrencyCode: t.Amount.CurrencyCode, MinorUnits: t.Amount.MinorUnits},
        BaseAmount: &budgetv1.Money{CurrencyCode: t.BaseAmount.CurrencyCode, MinorUnits: t.BaseAmount.MinorUnits},
        Fx:         fx,
        OccurredAt: timestamppb.New(t.OccurredAt),
        Comment:    t.Comment,
        CreatedAt:  timestamppb.New(t.CreatedAt),
    }
}

func mapTxType(t budgetv1.TransactionType) domain.TransactionType {
    switch t {
    case budgetv1.TransactionType_TRANSACTION_TYPE_INCOME:
        return domain.TransactionTypeIncome
    case budgetv1.TransactionType_TRANSACTION_TYPE_EXPENSE:
        return domain.TransactionTypeExpense
    default:
        return ""
    }
}

func toProtoTxType(t domain.TransactionType) budgetv1.TransactionType {
    switch t {
    case domain.TransactionTypeIncome:
        return budgetv1.TransactionType_TRANSACTION_TYPE_INCOME
    case domain.TransactionTypeExpense:
        return budgetv1.TransactionType_TRANSACTION_TYPE_EXPENSE
    default:
        return budgetv1.TransactionType_TRANSACTION_TYPE_UNSPECIFIED
    }
}

func applyFieldMask(cur *domain.Transaction, patch *budgetv1.Transaction, mask *fieldmaskpb.FieldMask) {
    if patch == nil || mask == nil {
        return
    }
    for _, p := range mask.Paths {
        switch p {
        case "category_id":
            cur.CategoryID = patch.GetCategoryId()
        case "type":
            cur.Type = mapTxType(patch.GetType())
        case "amount":
            if patch.GetAmount() != nil {
                cur.Amount.CurrencyCode = patch.GetAmount().GetCurrencyCode()
                cur.Amount.MinorUnits = patch.GetAmount().GetMinorUnits()
            }
        case "occurred_at":
            if patch.GetOccurredAt() != nil {
                cur.OccurredAt = patch.GetOccurredAt().AsTime()
            }
        case "comment":
            cur.Comment = patch.GetComment()
        }
    }
}

func maskAffectsBase(mask *fieldmaskpb.FieldMask) bool {
    if mask == nil {
        return false
    }
    for _, p := range mask.Paths {
        if p == "amount" || p == "occurred_at" {
            return true
        }
    }
    return false
}


