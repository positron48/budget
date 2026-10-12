package grpcadapter

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

// Empty configuration permits all tenants through this rollout gate; normal
// membership/role checks still apply. Invalid configuration fails startup.
func InvestmentTenantRolloutUnaryInterceptor(configuration string) (grpc.UnaryServerInterceptor, error) {
	allowed := map[string]bool{}
	if strings.TrimSpace(configuration) != "" {
		for _, part := range strings.Split(configuration, ",") {
			id := strings.TrimSpace(part)
			parsed, err := uuid.Parse(id)
			if err != nil || parsed.String() != id {
				return nil, fmt.Errorf("INVESTMENTS_TENANT_ALLOWLIST must contain canonical UUIDs separated by commas")
			}
			allowed[id] = true
		}
	}
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if len(allowed) > 0 && isInvestmentMethod(info.FullMethod) {
			tenant, ok := ctxutil.TenantIDFromContext(ctx)
			if !ok || tenant == "" {
				return nil, status.Error(codes.Unauthenticated, "missing tenant context")
			}
			if !allowed[tenant] {
				return nil, status.Error(codes.PermissionDenied, "investment module is not enabled for this organization")
			}
		}
		return handler(ctx, req)
	}, nil
}
func isInvestmentMethod(method string) bool {
	for _, service := range []string{"InvestmentService", "InvestmentImportService", "InvestmentNAVService", "MarketDataService"} {
		if strings.HasPrefix(method, "/budget.v1."+service+"/") {
			return true
		}
	}
	return false
}
