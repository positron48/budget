package auth

import (
    "context"
    "testing"
    "time"
)

func TestJWTIssuer_Issue(t *testing.T) {
    iss := NewJWTIssuer("k")
    tp, err := iss.Issue(context.Background(), "u1", "t1", time.Minute, time.Hour)
    if err != nil { t.Fatalf("issue: %v", err) }
    if tp.AccessToken == "" || tp.RefreshToken == "" { t.Fatal("empty tokens") }
    if tp.TokenType != "Bearer" { t.Fatalf("unexpected type: %s", tp.TokenType) }
    if tp.AccessTokenExpiresAt.Before(time.Now()) || tp.RefreshTokenExpiresAt.Before(time.Now()) {
        t.Fatal("expiration not set correctly")
    }
}


