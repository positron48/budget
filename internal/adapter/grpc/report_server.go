//go:build !ignore
// +build !ignore

package grpcadapter

import (
	"context"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	repuse "github.com/positron48/budget/internal/usecase/report"
)

type ReportServer struct {
	budgetv1.UnimplementedReportServiceServer
    svc interface{ GetMonthlySummary(ctx context.Context, tenantID string, year int, month int, locale string, targetCurrencyCode string) (repuse.MonthlySummary, error) }
}

func NewReportServer(svc interface{ GetMonthlySummary(ctx context.Context, tenantID string, year int, month int, locale string, targetCurrencyCode string) (repuse.MonthlySummary, error) }) *ReportServer { return &ReportServer{svc: svc} }

func (s *ReportServer) GetMonthlySummary(ctx context.Context, req *budgetv1.GetMonthlySummaryRequest) (*budgetv1.GetMonthlySummaryResponse, error) {
	tenantID, _ := ctxutil.TenantIDFromContext(ctx)
	sum, err := s.svc.GetMonthlySummary(ctx, tenantID, int(req.GetYear()), int(req.GetMonth()), req.GetLocale(), req.GetTargetCurrencyCode())
	if err != nil {
		return nil, mapError(err)
	}
	items := make([]*budgetv1.MonthlyCategorySummaryItem, 0, len(sum.Items))
	for _, it := range sum.Items {
		items = append(items, &budgetv1.MonthlyCategorySummaryItem{
			CategoryId:   it.CategoryID,
			CategoryName: it.CategoryName,
			Type:         toProtoTxType(it.Type),
			Total:        &budgetv1.Money{CurrencyCode: it.Total.CurrencyCode, MinorUnits: it.Total.MinorUnits},
		})
	}
	return &budgetv1.GetMonthlySummaryResponse{
		Items:        items,
		TotalIncome:  &budgetv1.Money{CurrencyCode: sum.TotalIncome.CurrencyCode, MinorUnits: sum.TotalIncome.MinorUnits},
		TotalExpense: &budgetv1.Money{CurrencyCode: sum.TotalExpense.CurrencyCode, MinorUnits: sum.TotalExpense.MinorUnits},
	}, nil
}
