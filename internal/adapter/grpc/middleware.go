package grpcadapter

import (
	"context"
	"strings"

	"github.com/positron48/budget/internal/pkg/ctxutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// AuthUnaryInterceptor extracts user_id and tenant_id from metadata/JWT (JWT parsing TODO)
func AuthUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			// x-tenant-id pass-through
			if vals := md.Get("x-tenant-id"); len(vals) > 0 && vals[0] != "" {
				ctx = ctxutil.WithTenantID(ctx, vals[0])
			}
			// authorization: Bearer <token> — parsing TBD (plug your JWT validator and fill user_id)
			if vals := md.Get("authorization"); len(vals) > 0 {
				token := vals[0]
				if strings.HasPrefix(strings.ToLower(token), "bearer ") {
					// TODO: validate JWT and extract sub, tenant_id
					// For now, leave context as-is
					_ = token
				}
			}
		}
		return handler(ctx, req)
	}
}
