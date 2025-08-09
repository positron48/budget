package config

import (
    "fmt"
    "os"
    "time"
)

type Config struct {
    AppEnv        string
    GRPCAddr      string
    DatabaseURL   string
    JWTSignKey    string
    JWTAccessTTL  time.Duration
    JWTRefreshTTL time.Duration
}

func getenv(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}

func Load() (Config, error) {
    var cfg Config
    cfg.AppEnv = getenv("APP_ENV", "dev")
    cfg.GRPCAddr = getenv("GRPC_ADDR", ":8080")
    cfg.DatabaseURL = getenv("DATABASE_URL", "")
    cfg.JWTSignKey = getenv("JWT_SIGN_KEY", "")

    access := getenv("JWT_ACCESS_TTL", "15m")
    refresh := getenv("JWT_REFRESH_TTL", "720h")

    var err error
    if cfg.JWTAccessTTL, err = time.ParseDuration(access); err != nil {
        return Config{}, fmt.Errorf("parse JWT_ACCESS_TTL: %w", err)
    }
    if cfg.JWTRefreshTTL, err = time.ParseDuration(refresh); err != nil {
        return Config{}, fmt.Errorf("parse JWT_REFRESH_TTL: %w", err)
    }

    return cfg, nil
}


