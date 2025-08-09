package grpcadapter

import (
    "context"
    "testing"

    "go.uber.org/zap"
    "google.golang.org/grpc"
)

func TestLoggingInterceptor(t *testing.T) {
    lg, _ := zap.NewDevelopment()
    it := LoggingUnaryInterceptor(lg.Sugar())
    ctx := context.Background()
    _, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Method"}, handlerOK)
    if err != nil { t.Fatalf("unexpected: %v", err) }
}


