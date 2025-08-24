package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	t.Setenv("GRPC_ADDR", ":8080")
	t.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("JWT_SIGN_KEY", "test-secret-key")
	t.Setenv("JWT_ACCESS_TTL", "15m")
	t.Setenv("JWT_REFRESH_TTL", "168h")
	t.Setenv("OAUTH_AUTH_TOKEN_TTL", "5m")
	t.Setenv("OAUTH_SESSION_TTL", "24h")
	t.Setenv("OAUTH_VERIFICATION_CODE_TTL", "10m")
	t.Setenv("OAUTH_MAX_ATTEMPTS_PER_HOUR", "10")
	t.Setenv("OAUTH_MAX_ATTEMPTS_PER_10MIN", "3")
	t.Setenv("OAUTH_WEB_BASE_URL", "http://localhost:3000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.AppEnv != "dev" {
		t.Fatalf("expected dev, got %s", cfg.AppEnv)
	}
	if cfg.GRPCAddr == "" {
		t.Fatalf("GRPC addr should have default")
	}
	if cfg.JWTAccessTTL <= 0 || cfg.JWTRefreshTTL <= 0 {
		t.Fatalf("ttls must be > 0")
	}
}

func TestLoad_ParseTTLs(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	t.Setenv("GRPC_ADDR", ":8080")
	t.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("JWT_SIGN_KEY", "test-secret-key")
	t.Setenv("JWT_ACCESS_TTL", "10m")
	t.Setenv("JWT_REFRESH_TTL", "24h")
	t.Setenv("OAUTH_AUTH_TOKEN_TTL", "5m")
	t.Setenv("OAUTH_SESSION_TTL", "24h")
	t.Setenv("OAUTH_VERIFICATION_CODE_TTL", "10m")
	t.Setenv("OAUTH_MAX_ATTEMPTS_PER_HOUR", "10")
	t.Setenv("OAUTH_MAX_ATTEMPTS_PER_10MIN", "3")
	t.Setenv("OAUTH_WEB_BASE_URL", "http://localhost:3000")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.JWTAccessTTL != 10*time.Minute || cfg.JWTRefreshTTL != 24*time.Hour {
		t.Fatalf("unexpected TTLs: %v %v", cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	}
}
