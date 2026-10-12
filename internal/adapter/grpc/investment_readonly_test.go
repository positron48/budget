package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestInvestmentReadOnlyGate(t *testing.T) {
	methods := map[string]bool{}
	for _, service := range []*grpc.ServiceDesc{&pb.InvestmentService_ServiceDesc, &pb.InvestmentImportService_ServiceDesc, &pb.InvestmentNAVService_ServiceDesc, &pb.MarketDataService_ServiceDesc} {
		for _, method := range service.Methods {
			methods["/"+service.ServiceName+"/"+method.MethodName] = true
		}
	}
	for read := range investmentReadMethods {
		if !methods[read] {
			t.Fatal("stale read method", read)
		}
	}
	for _, method := range []string{"/budget.v1.InvestmentService/FutureMethod", "/budget.v1.AssetService/Save"} {
		methods[method] = true
	}
	for method := range methods {
		for _, readonly := range []bool{true, false} {
			called := false
			_, err := InvestmentReadOnlyUnaryInterceptor(readonly)(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: method}, func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil })
			blocked := readonly && isInvestmentMethod(method) && !investmentReadMethods[method]
			if blocked {
				if called || status.Code(err) != codes.FailedPrecondition {
					t.Fatal(method, err)
				}
			} else if !called || err != nil {
				t.Fatal(method, err)
			}
		}
	}
	for _, write := range []string{"/budget.v1.InvestmentService/MutateInvestmentCashTransfer", "/budget.v1.InvestmentService/PreviewInvestmentImportDraft", "/budget.v1.InvestmentImportService/PostBrokerTrade", "/budget.v1.InvestmentImportService/LinkBrokerTrade", "/budget.v1.MarketDataService/RequestMarketRefresh", "/budget.v1.InvestmentNAVService/RequestInvestmentNAVRefresh"} {
		if investmentReadMethods[write] {
			t.Fatal("write classified as read", write)
		}
	}
}
