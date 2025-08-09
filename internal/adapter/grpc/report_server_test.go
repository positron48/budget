package grpcadapter

import (
    "context"
    "errors"
    "testing"
    "time"

    budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
    "github.com/positron48/budget/internal/domain"
    repuse "github.com/positron48/budget/internal/usecase/report"
)

type stubReportService struct{}
func (stubReportService) GetMonthlySummary(ctx context.Context, tenantID string, year int, month int, locale string, targetCurrencyCode string) (repuse.MonthlySummary, error) {
    return repuse.MonthlySummary{Items: []repuse.MonthlyItem{{CategoryID: "c1", CategoryName: "Food", Type: domain.TransactionTypeExpense, Total: domain.Money{CurrencyCode: "RUB", MinorUnits: 100}}}, TotalExpense: domain.Money{CurrencyCode: "RUB", MinorUnits: 100}}, nil
}

func TestReportServer_GetMonthlySummary(t *testing.T) {
    srv := NewReportServer(&stubReportService{})
    resp, err := srv.GetMonthlySummary(context.Background(), &budgetv1.GetMonthlySummaryRequest{Year: 2025, Month: int32(time.Now().Month())})
    if err != nil || len(resp.GetItems()) != 1 { t.Fatalf("unexpected: %v %#v", err, resp) }
}

type errReportSvc struct{}
func (errReportSvc) GetMonthlySummary(ctx context.Context, tenantID string, year int, month int, locale string, targetCurrencyCode string) (repuse.MonthlySummary, error) { return repuse.MonthlySummary{}, errors.New("boom") }

func TestReportServer_Error(t *testing.T) {
    srv := NewReportServer(errReportSvc{})
    if _, err := srv.GetMonthlySummary(context.Background(), &budgetv1.GetMonthlySummaryRequest{Year: 2025, Month: 1}); err == nil { t.Fatal("expected error") }
}


