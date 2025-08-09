package ctxutil

import "context"

type ctxKey string

const (
	keyUserID   ctxKey = "user_id"
	keyTenantID ctxKey = "tenant_id"
)

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, keyUserID, userID)
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(keyUserID).(string)
	return v, ok && v != ""
}

func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, keyTenantID, tenantID)
}

func TenantIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(keyTenantID).(string)
	return v, ok && v != ""
}
