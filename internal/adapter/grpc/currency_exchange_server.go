package grpcadapter

import (
	"context"
	"errors"
	"time"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	currencyexchange "github.com/positron48/budget/internal/usecase/currencyexchange"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type CurrencyExchangeServer struct {
	budgetv1.UnimplementedCurrencyExchangeServiceServer
	svc interface {
		Create(context.Context, domain.CurrencyExchange) (domain.CurrencyExchange, error)
		List(context.Context, string, int, int) ([]domain.CurrencyExchange, int64, error)
		Delete(context.Context, string, string) error
	}
}

func NewCurrencyExchangeServer(svc interface {
	Create(context.Context, domain.CurrencyExchange) (domain.CurrencyExchange, error)
	List(context.Context, string, int, int) ([]domain.CurrencyExchange, int64, error)
	Delete(context.Context, string, string) error
}) *CurrencyExchangeServer {
	return &CurrencyExchangeServer{svc: svc}
}

func (s *CurrencyExchangeServer) CreateCurrencyExchange(ctx context.Context, req *budgetv1.CreateCurrencyExchangeRequest) (*budgetv1.CreateCurrencyExchangeResponse, error) {
	if req.GetFromAmount() == nil || req.GetToAmount() == nil {
		return nil, invalidArg("from_amount and to_amount are required")
	}
	tenantID, _ := ctxutil.TenantIDFromContext(ctx)
	userID, _ := ctxutil.UserIDFromContext(ctx)
	occurredAt := time.Now()
	if req.GetOccurredAt() != nil {
		occurredAt = req.GetOccurredAt().AsTime()
	}
	created, err := s.svc.Create(ctx, domain.CurrencyExchange{
		TenantID: tenantID,
		UserID:   userID,
		FromAmount: domain.Money{
			CurrencyCode: req.GetFromAmount().GetCurrencyCode(),
			MinorUnits:   req.GetFromAmount().GetMinorUnits(),
		},
		ToAmount: domain.Money{
			CurrencyCode: req.GetToAmount().GetCurrencyCode(),
			MinorUnits:   req.GetToAmount().GetMinorUnits(),
		},
		OccurredAt: occurredAt,
		Note:       req.GetNote(),
	})
	if err != nil {
		if errors.Is(err, currencyexchange.ErrInvalidAmount) || errors.Is(err, currencyexchange.ErrInvalidCurrency) {
			return nil, invalidArg(err.Error())
		}
		return nil, mapError(err)
	}
	return &budgetv1.CreateCurrencyExchangeResponse{Exchange: toProtoCurrencyExchange(created)}, nil
}

func (s *CurrencyExchangeServer) ListCurrencyExchanges(ctx context.Context, req *budgetv1.ListCurrencyExchangesRequest) (*budgetv1.ListCurrencyExchangesResponse, error) {
	tenantID, _ := ctxutil.TenantIDFromContext(ctx)
	page, pageSize := 1, 20
	if req.GetPage() != nil {
		page = int(req.GetPage().GetPage())
		pageSize = int(req.GetPage().GetPageSize())
	}
	items, total, err := s.svc.List(ctx, tenantID, page, pageSize)
	if err != nil {
		return nil, mapError(err)
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	out := make([]*budgetv1.CurrencyExchange, 0, len(items))
	for _, item := range items {
		out = append(out, toProtoCurrencyExchange(item))
	}
	totalPages := (total + int64(pageSize) - 1) / int64(pageSize)
	return &budgetv1.ListCurrencyExchangesResponse{
		Exchanges: out,
		Page: &budgetv1.PageResponse{
			Page:       int32(page),
			PageSize:   int32(pageSize),
			TotalItems: total,
			TotalPages: int32(totalPages),
		},
	}, nil
}

func (s *CurrencyExchangeServer) DeleteCurrencyExchange(ctx context.Context, req *budgetv1.DeleteCurrencyExchangeRequest) (*budgetv1.DeleteCurrencyExchangeResponse, error) {
	if req.GetId() == "" {
		return nil, invalidArg("id is required")
	}
	tenantID, _ := ctxutil.TenantIDFromContext(ctx)
	if err := s.svc.Delete(ctx, tenantID, req.GetId()); err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.DeleteCurrencyExchangeResponse{}, nil
}

func toProtoCurrencyExchange(item domain.CurrencyExchange) *budgetv1.CurrencyExchange {
	return &budgetv1.CurrencyExchange{
		Id:       item.ID,
		TenantId: item.TenantID,
		UserId:   item.UserID,
		FromAmount: &budgetv1.Money{
			CurrencyCode: item.FromAmount.CurrencyCode,
			MinorUnits:   item.FromAmount.MinorUnits,
		},
		ToAmount: &budgetv1.Money{
			CurrencyCode: item.ToAmount.CurrencyCode,
			MinorUnits:   item.ToAmount.MinorUnits,
		},
		RateDecimal: item.RateDecimal,
		OccurredAt:  timestamppb.New(item.OccurredAt),
		Note:        item.Note,
		CreatedAt:   timestamppb.New(item.CreatedAt),
	}
}
