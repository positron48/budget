package grpcadapter

import (
	"context"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestInvestmentTenantRollout(t *testing.T) {
	const allowed = "11111111-1111-4111-8111-111111111111"
	const denied = "22222222-2222-4222-8222-222222222222"
	gate, err := InvestmentTenantRolloutUnaryInterceptor(" " + allowed + ", " + allowed + " ")
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"InvestmentService", "InvestmentImportService", "InvestmentNAVService", "MarketDataService", "AssetService"} {
		for _, tenant := range []string{allowed, denied, ""} {
			called := false
			ctx := ctxutil.WithTenantID(context.Background(), tenant)
			_, err := gate(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1." + service + "/ReadOrWrite"}, func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil })
			expected := codes.OK
			if service != "AssetService" && tenant != allowed {
				expected = codes.PermissionDenied
				if tenant == "" {
					expected = codes.Unauthenticated
				}
			}
			if status.Code(err) != expected || called != (expected == codes.OK) {
				t.Fatal(service, tenant, called, err)
			}
		}
	}
	for _, bad := range []string{"*", allowed + ",", ",", "not-a-uuid"} {
		if _, err := InvestmentTenantRolloutUnaryInterceptor(bad); err == nil {
			t.Fatal(bad)
		}
	}
	gate, err = InvestmentTenantRolloutUnaryInterceptor("")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = gate(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.InvestmentService/List"}, func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil })
	if err != nil || !called {
		t.Fatal(err)
	}
}

func TestInvestmentRolloutDoesNotReplaceMembership(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	gate, err := InvestmentTenantRolloutUnaryInterceptor(tenant)
	if err != nil {
		t.Fatal(err)
	}
	membership := NewTenantGuardUnaryInterceptor(func(context.Context, string, string) (bool, error) { return false, nil })
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), tenant), "outside-user")
	info := &grpc.UnaryServerInfo{FullMethod: "/budget.v1.InvestmentImportService/PostBrokerTrade"}
	called := false
	_, err = gate(ctx, nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return membership(ctx, req, info, func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil })
	})
	if called || status.Code(err) != codes.PermissionDenied {
		t.Fatal(called, err)
	}
}
