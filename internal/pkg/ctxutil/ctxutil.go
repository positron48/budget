package ctxutil

import "context"

type ctxKey string

const (
	keyUserID           ctxKey = "user_id"
	keyTenantID         ctxKey = "tenant_id"
	keyFinancialRequest ctxKey = "financial_request"
	keyAssetAccount     ctxKey = "asset_account"
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

type FinancialRequest struct {
	Key     string
	Payload any
}

func WithFinancialRequest(ctx context.Context, key string, payload any) context.Context {
	return context.WithValue(ctx, keyFinancialRequest, FinancialRequest{key, payload})
}
func FinancialRequestFromContext(ctx context.Context) FinancialRequest {
	v, _ := ctx.Value(keyFinancialRequest).(FinancialRequest)
	return v
}
func WithAssetAccount(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyAssetAccount, id)
}
func AssetAccountFromContext(ctx context.Context) string {
	v, _ := ctx.Value(keyAssetAccount).(string)
	return v
}
