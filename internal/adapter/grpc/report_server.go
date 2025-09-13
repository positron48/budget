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
	svc interface {
		GetMonthlySummary(ctx context.Context, tenantID string, year int, month int, locale string, targetCurrencyCode string, tzOffsetMinutes int) (repuse.MonthlySummary, error)
		GetSummaryReport(ctx context.Context, tenantID string, fromDate, toDate, locale, targetCurrencyCode string, tzOffsetMinutes int) (repuse.SummaryReport, error)
		GetDateRange(ctx context.Context, tenantID string, locale string, tzOffsetMinutes int) (repuse.DateRange, error)
	}
}

func NewReportServer(svc interface {
	GetMonthlySummary(ctx context.Context, tenantID string, year int, month int, locale string, targetCurrencyCode string, tzOffsetMinutes int) (repuse.MonthlySummary, error)
	GetSummaryReport(ctx context.Context, tenantID string, fromDate, toDate, locale, targetCurrencyCode string, tzOffsetMinutes int) (repuse.SummaryReport, error)
	GetDateRange(ctx context.Context, tenantID string, locale string, tzOffsetMinutes int) (repuse.DateRange, error)
},
) *ReportServer {
	return &ReportServer{svc: svc}
}

func (s *ReportServer) GetMonthlySummary(ctx context.Context, req *budgetv1.GetMonthlySummaryRequest) (*budgetv1.GetMonthlySummaryResponse, error) {
	tenantID, _ := ctxutil.TenantIDFromContext(ctx)
	sum, err := s.svc.GetMonthlySummary(ctx, tenantID, int(req.GetYear()), int(req.GetMonth()), req.GetLocale(), req.GetTargetCurrencyCode(), int(req.GetTimezoneOffsetMinutes()))
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

func (s *ReportServer) GetSummaryReport(ctx context.Context, req *budgetv1.GetSummaryReportRequest) (*budgetv1.GetSummaryReportResponse, error) {
	tenantID, _ := ctxutil.TenantIDFromContext(ctx)
	report, err := s.svc.GetSummaryReport(ctx, tenantID, req.GetFromDate(), req.GetToDate(), req.GetLocale(), req.GetTargetCurrencyCode(), int(req.GetTimezoneOffsetMinutes()))
	if err != nil {
		return nil, mapError(err)
	}
	
	categories := make([]*budgetv1.MonthlyCategoryData, 0, len(report.Categories))
	for _, cat := range report.Categories {
		monthlyTotals := make([]*budgetv1.Money, 0, len(cat.MonthlyTotals))
		for _, total := range cat.MonthlyTotals {
			monthlyTotals = append(monthlyTotals, &budgetv1.Money{
				CurrencyCode: total.CurrencyCode,
				MinorUnits:   total.MinorUnits,
			})
		}
		
		categories = append(categories, &budgetv1.MonthlyCategoryData{
			CategoryId:    cat.CategoryID,
			CategoryName:  cat.CategoryName,
			Type:          toProtoTxType(cat.Type),
			MonthlyTotals: monthlyTotals,
			Total:         &budgetv1.Money{CurrencyCode: cat.Total.CurrencyCode, MinorUnits: cat.Total.MinorUnits},
		})
	}
	
	return &budgetv1.GetSummaryReportResponse{
		Categories:   categories,
		Months:       report.Months,
		TotalIncome:  &budgetv1.Money{CurrencyCode: report.TotalIncome.CurrencyCode, MinorUnits: report.TotalIncome.MinorUnits},
		TotalExpense: &budgetv1.Money{CurrencyCode: report.TotalExpense.CurrencyCode, MinorUnits: report.TotalExpense.MinorUnits},
	}, nil
}

func (s *ReportServer) GetDateRange(ctx context.Context, req *budgetv1.GetDateRangeRequest) (*budgetv1.GetDateRangeResponse, error) {
	tenantID, _ := ctxutil.TenantIDFromContext(ctx)
	dateRange, err := s.svc.GetDateRange(ctx, tenantID, req.GetLocale(), int(req.GetTimezoneOffsetMinutes()))
	if err != nil {
		return nil, mapError(err)
	}
	
	return &budgetv1.GetDateRangeResponse{
		EarliestDate: dateRange.EarliestDate,
		LatestDate:   dateRange.LatestDate,
	}, nil
}
