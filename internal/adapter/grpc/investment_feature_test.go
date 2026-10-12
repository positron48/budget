package grpcadapter

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestInvestmentFeatureGate(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, service := range []string{"InvestmentService", "InvestmentImportService", "InvestmentNAVService", "MarketDataService", "AssetService", "AuthService", "InvestmentServiceOther"} {
			called := false
			_, err := InvestmentFeatureUnaryInterceptor(enabled)(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1." + service + "/Method"}, func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil })
			blocked := !enabled && (service == "InvestmentService" || service == "InvestmentImportService" || service == "InvestmentNAVService" || service == "MarketDataService")
			if blocked {
				if called || status.Code(err) != codes.Unavailable {
					t.Fatal(service, called, err)
				}
			} else if !called || err != nil {
				t.Fatal(service, called, err)
			}
		}
	}
}
