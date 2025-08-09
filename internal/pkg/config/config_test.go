package config

import (
	"os"
	"testing"
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
