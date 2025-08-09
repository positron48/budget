package config

import (
    "os"
    "testing"
)

func TestLoad_Defaults(t *testing.T) {
    os.Unsetenv("APP_ENV")
    os.Unsetenv("GRPC_ADDR")
    os.Unsetenv("DATABASE_URL")
    os.Unsetenv("JWT_SIGN_KEY")
    os.Unsetenv("JWT_ACCESS_TTL")
    os.Unsetenv("JWT_REFRESH_TTL")

    cfg, err := Load()
    if err != nil { t.Fatalf("Load() error: %v", err) }
    if cfg.AppEnv != "dev" { t.Fatalf("expected dev, got %s", cfg.AppEnv) }
    if cfg.GRPCAddr == "" { t.Fatalf("GRPC addr should have default") }
    if cfg.JWTAccessTTL <= 0 || cfg.JWTRefreshTTL <= 0 { t.Fatalf("ttls must be > 0") }
}


