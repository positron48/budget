package grpcadapter

import (
    "context"
    "testing"

    "go.uber.org/zap"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

func TestLoggingInterceptor(t *testing.T) {
    lg, _ := zap.NewDevelopment()
    it := LoggingUnaryInterceptor(lg.Sugar())
    ctx := context.Background()
    _, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Method"}, handlerOK)
    if err != nil { t.Fatalf("unexpected: %v", err) }
}

func handlerErr(ctx context.Context, req interface{}) (interface{}, error) { return nil, status.Error(codes.InvalidArgument, "bad") }

func TestLoggingInterceptor_Error(t *testing.T) {
    lg, _ := zap.NewDevelopment()
    it := LoggingUnaryInterceptor(lg.Sugar())
    ctx := context.Background()
    _, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Method"}, handlerErr)
    if status.Code(err) != codes.InvalidArgument { t.Fatalf("expected InvalidArgument, got %v", err) }
}


