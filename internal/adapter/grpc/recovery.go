package grpcadapter

import (
	"context"
	"runtime/debug"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RecoveryUnaryInterceptor catches panics, logs them, and returns Internal error.
func RecoveryUnaryInterceptor(lg *zap.SugaredLogger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		started := time.Now()
		defer func() {
			if r := recover(); r != nil {
				lg.Errorw("panic recovered", "method", info.FullMethod, "panic", r, "stack", string(debug.Stack()))
				err = status.Error(codes.Internal, "internal server error")
			}
			// optional: basic timing log on error
			if err != nil {
				lg.Infow("rpc", "method", info.FullMethod, "elapsed_ms", time.Since(started).Milliseconds(), "error", err)
			}
		}()
		return handler(ctx, req)
	}
}
