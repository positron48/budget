package grpcadapter

import (
    "context"
    "testing"

    "go.uber.org/zap"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

func panicHandler(ctx context.Context, req interface{}) (interface{}, error) { panic("boom") }

func TestRecoveryInterceptor(t *testing.T) {
    lg, _ := zap.NewDevelopment()
    it := RecoveryUnaryInterceptor(lg.Sugar())
    _, err := it(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/test"}, panicHandler)
    if status.Code(err) != codes.Internal { t.Fatalf("expected Internal, got %v", err) }
}


