package grpcadapter

import (
	"context"
	"time"

	"github.com/positron48/budget/internal/pkg/ctxutil"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// LoggingUnaryInterceptor logs method, duration, status code, and user/tenant ids.
func LoggingUnaryInterceptor(lg *zap.SugaredLogger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		started := time.Now()
		resp, err := handler(ctx, req)
		code := status.Code(err)
		userID, _ := ctxutil.UserIDFromContext(ctx)
		tenantID, _ := ctxutil.TenantIDFromContext(ctx)
		lg.Infow("rpc",
			"method", info.FullMethod,
			"elapsed_ms", time.Since(started).Milliseconds(),
			"code", code.String(),
			"user_id", userID,
			"tenant_id", tenantID,
		)
		return resp, err
	}
}
