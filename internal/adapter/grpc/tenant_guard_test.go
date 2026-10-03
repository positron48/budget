package grpcadapter

import (
	"context"
	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"testing"

	"github.com/positron48/budget/internal/pkg/ctxutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTenantGuard_AllowsPublic(t *testing.T) {
	it := NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return true, nil })
	_, err := it(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.AuthService/Login"}, handlerOK)
	if err != nil {
		t.Fatalf("public/pass-through should succeed: %v", err)
	}
}

func TestTenantGuard_DeniesNonMember(t *testing.T) {
	it := NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return false, nil })
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "t1"), "u1")
	_, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.CategoryService/ListCategories"}, handlerOK)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestTenantGuard_ErrorFromValidate(t *testing.T) {
	it := NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return false, assertErr{} })
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "t1"), "u1")
	_, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.ReportService/GetMonthlySummary"}, handlerOK)
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected Internal, got %v", err)
	}
}

type assertErr struct{}

func (assertErr) Error() string { return "assert" }

func TestTenantGuard_MissingContext(t *testing.T) {
	it := NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return true, nil })
	// no user/tenant in ctx
	_, err := it(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.TransactionService/ListTransactions"}, handlerOK)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", err)
	}
}

func TestTenantGuard_CurrencyExchangeIsTenantScoped(t *testing.T) {
	it := NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return false, nil })
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "t1"), "u1")
	_, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.CurrencyExchangeService/ListCurrencyExchanges"}, handlerOK)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestTenantGuard_AllAssetRPCs(t *testing.T) {
	for _, method := range budgetv1.AssetService_ServiceDesc.Methods {
		t.Run(method.MethodName, func(t *testing.T) {
			called := false
			handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }
			guard := NewTenantGuardUnaryInterceptor(func(context.Context, string, string) (bool, error) { return false, nil })
			info := &grpc.UnaryServerInfo{FullMethod: "/budget.v1.AssetService/" + method.MethodName}
			_, err := guard(context.Background(), nil, info, handler)
			if status.Code(err) != codes.Unauthenticated {
				t.Fatalf("missing auth: %v", err)
			}
			ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "foreign"), "user")
			_, err = guard(ctx, nil, info, handler)
			if status.Code(err) != codes.PermissionDenied || called {
				t.Fatalf("foreign tenant reached handler: %v", err)
			}
		})
	}
}
