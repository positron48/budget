package grpcadapter

import (
    "context"
    "testing"
    "time"

    jwt "github.com/golang-jwt/jwt/v5"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/metadata"
    "google.golang.org/grpc/status"
)

func handlerOK(ctx context.Context, req interface{}) (interface{}, error) { return "ok", nil }

func TestAuthInterceptor_Public(t *testing.T) {
    it := NewAuthUnaryInterceptor("k")
    // health is public
    _, err := it(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}, handlerOK)
    if err != nil { t.Fatalf("public method should pass: %v", err) }
}

func TestAuthInterceptor_Protected_NoToken(t *testing.T) {
    it := NewAuthUnaryInterceptor("k")
    _, err := it(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.CategoryService/ListCategories"}, handlerOK)
    if status.Code(err) != codes.Unauthenticated { t.Fatalf("expected Unauthenticated, got %v", err) }
}

func TestAuthInterceptor_Protected_WithToken(t *testing.T) {
    it := NewAuthUnaryInterceptor("k")
    claims := jwt.MapClaims{"sub": "u1", "tenant_id": "t1", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()}
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    s, _ := token.SignedString([]byte("k"))
    ctx := metadataIncoming(map[string]string{"authorization": "Bearer " + s})
    _, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.CategoryService/ListCategories"}, handlerOK)
    if err != nil { t.Fatalf("unexpected error: %v", err) }
}

func metadataIncoming(m map[string]string) context.Context {
    return metadata.NewIncomingContext(context.Background(), metadata.New(m))
}


