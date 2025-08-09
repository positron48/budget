package ctxutil

import (
    "context"
    "testing"
)

func TestUserTenantContext(t *testing.T) {
    ctx := context.Background()
    if _, ok := UserIDFromContext(ctx); ok { t.Fatal("expected no user id") }
    if _, ok := TenantIDFromContext(ctx); ok { t.Fatal("expected no tenant id") }

    ctx = WithUserID(ctx, "u1")
    ctx = WithTenantID(ctx, "t1")
    if uid, ok := UserIDFromContext(ctx); !ok || uid != "u1" { t.Fatalf("want u1, got %q, ok=%v", uid, ok) }
    if tid, ok := TenantIDFromContext(ctx); !ok || tid != "t1" { t.Fatalf("want t1, got %q, ok=%v", tid, ok) }
}


