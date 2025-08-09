package grpcadapter

import (
    "context"
    "testing"
    "time"

    jwt "github.com/golang-jwt/jwt/v5"
    "github.com/positron48/budget/internal/pkg/ctxutil"
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

func TestAuthInterceptor_NoMetadata(t *testing.T) {
    it := NewAuthUnaryInterceptor("k")
    // ensure path with nil metadata also returns Unauthenticated
    _, err := it(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.TransactionService/ListTransactions"}, handlerOK)
    if status.Code(err) != codes.Unauthenticated { t.Fatalf("expected Unauthenticated, got %v", err) }
}

func TestAuthInterceptor_Protected_WithToken(t *testing.T) {
    it := NewAuthUnaryInterceptor("k")
    claims := jwt.MapClaims{"sub": "u1", "tenant_id": "t1", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()}
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    s, _ := token.SignedString([]byte("k"))
    ctx := metadataIncoming(map[string]string{"authorization": "Bearer " + s, "x-tenant-id": "t1"})
    _, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.CategoryService/ListCategories"}, handlerOK)
    if err != nil { t.Fatalf("unexpected error: %v", err) }
}

func TestAuthInterceptor_InvalidSignature(t *testing.T) {
    it := NewAuthUnaryInterceptor("k")
    // sign with different key
    claims := jwt.MapClaims{"sub": "u1", "tenant_id": "t1", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()}
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    s, _ := token.SignedString([]byte("wrong"))
    ctx := metadataIncoming(map[string]string{"authorization": "Bearer " + s})
    _, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.CategoryService/ListCategories"}, handlerOK)
    if status.Code(err) != codes.Unauthenticated { t.Fatalf("expected Unauthenticated, got %v", err) }
}

func TestAuthInterceptor_ExpiredToken(t *testing.T) {
    it := NewAuthUnaryInterceptor("k")
    claims := jwt.MapClaims{"sub": "u1", "tenant_id": "t1", "iat": time.Now().Add(-2*time.Minute).Unix(), "exp": time.Now().Add(-time.Minute).Unix()}
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    s, _ := token.SignedString([]byte("k"))
    ctx := metadataIncoming(map[string]string{"authorization": "Bearer " + s})
    _, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.TransactionService/ListTransactions"}, handlerOK)
    // jwt-go validates exp; expect Unauthenticated
    if status.Code(err) != codes.Unauthenticated { t.Fatalf("expected Unauthenticated for expired, got %v", err) }
}

func TestAuthInterceptor_BadScheme(t *testing.T) {
    it := NewAuthUnaryInterceptor("k")
    ctx := metadataIncoming(map[string]string{"authorization": "Token abc"})
    _, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.CategoryService/ListCategories"}, handlerOK)
    if status.Code(err) != codes.Unauthenticated { t.Fatalf("expected Unauthenticated for bad scheme, got %v", err) }
}

func TestAuthInterceptor_TenantOverride(t *testing.T) {
    it := NewAuthUnaryInterceptor("k")
    claims := jwt.MapClaims{"sub": "u1", "tenant_id": "claim-tenant", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()}
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    s, _ := token.SignedString([]byte("k"))
    var gotTenant string
    handler := func(ctx context.Context, req interface{}) (interface{}, error) {
        if md, ok := metadata.FromIncomingContext(ctx); ok { _ = md }
        tID, _ := ctxTenant(ctx)
        gotTenant = tID
        return "ok", nil
    }
    ctxTenantID := "meta-tenant"
    ctx := metadataIncoming(map[string]string{"authorization": "Bearer " + s, "x-tenant-id": ctxTenantID})
    _, err := it(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/budget.v1.ReportService/GetMonthlySummary"}, handler)
    if err != nil { t.Fatalf("unexpected: %v", err) }
    if gotTenant != ctxTenantID { t.Fatalf("x-tenant-id should override claims, got %q", gotTenant) }
}

// helper to extract tenant id from ctx using package function
func ctxTenant(ctx context.Context) (string, bool) {
    return ctxutil.TenantIDFromContext(ctx)
}

func metadataIncoming(m map[string]string) context.Context {
    return metadata.NewIncomingContext(context.Background(), metadata.New(m))
}


