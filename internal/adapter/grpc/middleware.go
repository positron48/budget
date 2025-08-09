package grpcadapter

import (
	"context"
	"strings"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// NewAuthUnaryInterceptor validates JWT (HS256) and extracts user_id/tenant_id into context.
func NewAuthUnaryInterceptor(signKey string) grpc.UnaryServerInterceptor {
	keyBytes := []byte(signKey)
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get("x-tenant-id"); len(vals) > 0 && vals[0] != "" {
				ctx = ctxutil.WithTenantID(ctx, vals[0])
			}
			if vals := md.Get("authorization"); len(vals) > 0 {
				token := vals[0]
				if strings.HasPrefix(strings.ToLower(token), "bearer ") {
					raw := strings.TrimSpace(token[len("Bearer "):])
					parsed, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
						if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
							return nil, jwt.ErrSignatureInvalid
						}
						return keyBytes, nil
					})
					if err == nil && parsed != nil && parsed.Valid {
						if claims, ok := parsed.Claims.(jwt.MapClaims); ok {
							if sub, ok := claims["sub"].(string); ok && sub != "" {
								ctx = ctxutil.WithUserID(ctx, sub)
							}
							if tid, ok := claims["tenant_id"].(string); ok && tid != "" {
								ctx = ctxutil.WithTenantID(ctx, tid)
							}
						}
					}
				}
			}
		}
		return handler(ctx, req)
	}
}
