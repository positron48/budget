package grpcadapter

import (
    "context"

    "github.com/positron48/budget/internal/pkg/ctxutil"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

// NewTenantGuardUnaryInterceptor ensures the authenticated user is a member of the active tenant
// for tenant-scoped RPCs (Category, Transaction, Report). Non-tenant-scoped methods are bypassed.
func NewTenantGuardUnaryInterceptor(validate func(ctx context.Context, userID, tenantID string) (bool, error)) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        if !isTenantScopedMethod(info.FullMethod) {
            return handler(ctx, req)
        }
        userID, okU := ctxutil.UserIDFromContext(ctx)
        tenantID, okT := ctxutil.TenantIDFromContext(ctx)
        if !okU || !okT || userID == "" || tenantID == "" {
            return nil, status.Error(codes.Unauthenticated, "missing user or tenant context")
        }
        ok, err := validate(ctx, userID, tenantID)
        if err != nil {
            return nil, status.Error(codes.Internal, "membership check failed")
        }
        if !ok {
            return nil, status.Error(codes.PermissionDenied, "user is not a member of the tenant")
        }
        return handler(ctx, req)
    }
}

func isTenantScopedMethod(fullMethod string) bool {
    switch {
    case hasPrefix(fullMethod, "/budget.v1.CategoryService/"):
        return true
    case hasPrefix(fullMethod, "/budget.v1.TransactionService/"):
        return true
    case hasPrefix(fullMethod, "/budget.v1.ReportService/"):
        return true
    default:
        return false
    }
}

func hasPrefix(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }


