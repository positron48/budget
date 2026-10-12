package grpcadapter

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// InvestmentFeatureUnaryInterceptor disables the whole API without touching
// persisted journals. Worker processes have their own explicit startup controls.
func InvestmentFeatureUnaryInterceptor(enabled bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if !enabled && isInvestmentMethod(info.FullMethod) {
			return nil, status.Error(codes.Unavailable, "investment module is disabled")
		}

		return handler(ctx, req)
	}
}
