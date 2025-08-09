package config

import (
    "os"
    "testing"
    "time"
)

func TestLoad_Defaults(t *testing.T) {
	_ = os.Unsetenv("APP_ENV")
	_ = os.Unsetenv("GRPC_ADDR")
	_ = os.Unsetenv("DATABASE_URL")
	_ = os.Unsetenv("JWT_SIGN_KEY")
	_ = os.Unsetenv("JWT_ACCESS_TTL")
	_ = os.Unsetenv("JWT_REFRESH_TTL")

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
    t.Setenv("JWT_ACCESS_TTL", "10m")
    t.Setenv("JWT_REFRESH_TTL", "24h")
    cfg, err := Load()
    if err != nil { t.Fatalf("Load() error: %v", err) }
    if cfg.JWTAccessTTL != 10*time.Minute || cfg.JWTRefreshTTL != 24*time.Hour {
        t.Fatalf("unexpected TTLs: %v %v", cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
    }
}
