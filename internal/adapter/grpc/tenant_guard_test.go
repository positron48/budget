package grpcadapter

import (
    "context"
    "testing"

    "github.com/positron48/budget/internal/pkg/ctxutil"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

func TestTenantGuard_AllowsPublic(t *testing.T) {
    it := NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return true, nil })
    _, err := it(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.AuthService/Login"}, handlerOK)
    if err != nil { t.Fatalf("public/pass-through should succeed: %v", err) }
}

func TestTenantGuard_DeniesNonMember(t *testing.T) {
    it := NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return false, nil })
    ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "t1"), "u1")
    _, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.CategoryService/ListCategories"}, handlerOK)
    if status.Code(err) != codes.PermissionDenied { t.Fatalf("expected PermissionDenied, got %v", err) }
}


