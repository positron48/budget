package grpcadapter

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Explicit classification: newly added investment RPCs are blocked until
// reviewed. PreviewImportDraft persists its review and is therefore a write.
var investmentReadMethods = map[string]bool{
	"/budget.v1.InvestmentNAVService/GetTrackerPerformance":                true,
	"/budget.v1.InvestmentNAVService/GetTrackerHistory":                    true,
	"/budget.v1.InvestmentNAVService/GetInvestmentQuotes":                  true,
	"/budget.v1.InvestmentNAVService/GetInvestmentExchangeRates":           true,
	"/budget.v1.InvestmentService/StartInvestmentExport":                   true, // persists only an expiring read artifact
	"/budget.v1.InvestmentService/GetInvestmentExport":                     true,
	"/budget.v1.InvestmentService/DownloadInvestmentExport":                true,
	"/budget.v1.MarketDataService/GetMarketDividendFacts":                  true,
	"/budget.v1.InvestmentService/GetInvestmentDividendGrowth":             true,
	"/budget.v1.InvestmentService/GetInvestmentForecast":                   true,
	"/budget.v1.InvestmentService/PreviewInvestmentLotQuantity":            true,
	"/budget.v1.InvestmentService/PreviewInvestmentTransfer":               true,
	"/budget.v1.InvestmentService/PreviewInvestmentCashTransfer":           true,
	"/budget.v1.InvestmentService/PreviewInvestmentOperation":              true,
	"/budget.v1.InvestmentService/GetInvestmentLedgerHistory":              true,
	"/budget.v1.InvestmentService/QueryInvestmentOperations":               true,
	"/budget.v1.InvestmentService/GetInvestmentOperationHistory":           true,
	"/budget.v1.InvestmentService/GetInvestmentPayouts":                    true,
	"/budget.v1.InvestmentService/GetInvestmentCashTransfer":               true,
	"/budget.v1.InvestmentService/PrepareInvestmentAssetActivation":        true,
	"/budget.v1.InvestmentNAVService/GetInvestmentLots":                    true,
	"/budget.v1.MarketDataService/GetMarketBondFacts":                      true,
	"/budget.v1.InvestmentNAVService/GetInvestmentCurrentNAV":              true,
	"/budget.v1.InvestmentNAVService/GetInvestmentNAVVariant":              true,
	"/budget.v1.InvestmentNAVService/ExportInvestmentSeries":               true,
	"/budget.v1.InvestmentNAVService/GetInvestmentOverview":                true,
	"/budget.v1.InvestmentNAVService/GetInvestmentProfit":                  true,
	"/budget.v1.InvestmentNAVService/GetInvestmentInstrumentPrices":        true,
	"/budget.v1.InvestmentNAVService/GetInvestmentPositions":               true,
	"/budget.v1.InvestmentNAVService/GetInvestmentInstrument":              true,
	"/budget.v1.InvestmentNAVService/GetInvestmentCompositionHistory":      true,
	"/budget.v1.InvestmentNAVService/GetInvestmentGroups":                  true,
	"/budget.v1.InvestmentService/GetInvestmentImportDraft":                true,
	"/budget.v1.InvestmentService/ListInvestmentImportDrafts":              true,
	"/budget.v1.InvestmentService/GetInvestmentTransfer":                   true,
	"/budget.v1.InvestmentService/ListInvestmentLedgers":                   true,
	"/budget.v1.InvestmentService/GetInvestmentSnapshot":                   true,
	"/budget.v1.InvestmentService/ListInvestmentOperations":                true,
	"/budget.v1.InvestmentImportService/PreviewBrokerTrade":                true,
	"/budget.v1.InvestmentImportService/ListBrokerSnapshotRows":            true,
	"/budget.v1.InvestmentImportService/ListBrokerSnapshots":               true,
	"/budget.v1.InvestmentImportService/GetBrokerSnapshot":                 true,
	"/budget.v1.InvestmentImportService/PreviewInvestmentFinancialMapping": true,
	"/budget.v1.InvestmentImportService/ListInvestmentImports":             true,
	"/budget.v1.InvestmentImportService/GetInvestmentImport":               true,
	"/budget.v1.InvestmentImportService/ListInvestmentImportRows":          true,
	"/budget.v1.InvestmentNAVService/ListInvestmentBenchmarks":             true,
	"/budget.v1.InvestmentNAVService/GetInvestmentBenchmarkComparison":     true,
	"/budget.v1.InvestmentNAVService/GetInvestmentPerformance":             true,
	"/budget.v1.InvestmentNAVService/GetInvestmentNAVSeries":               true,
	"/budget.v1.InvestmentNAVService/ListInvestmentPortfolios":             true,
	"/budget.v1.InvestmentNAVService/ListInvestmentValuationProfiles":      true,
	"/budget.v1.InvestmentNAVService/ListInvestmentNAVSeries":              true,
	"/budget.v1.InvestmentNAVService/GetInvestmentNAVHistory":              true,
	"/budget.v1.MarketDataService/GetInvestmentMarketMapping":              true,
	"/budget.v1.MarketDataService/SearchMarketInstruments":                 true,
	"/budget.v1.MarketDataService/ListMarketSources":                       true,
	"/budget.v1.MarketDataService/GetMarketHistory":                        true,
	"/budget.v1.MarketDataService/GetMarketDataJob":                        true,
}

func InvestmentReadOnlyUnaryInterceptor(readOnly bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if readOnly && isInvestmentMethod(info.FullMethod) && !investmentReadMethods[info.FullMethod] {
			return nil, status.Error(codes.FailedPrecondition, "investment module is read-only")
		}
		return handler(ctx, req)
	}
}
