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
	RedisURL      string
	JWTSignKey    string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration
	MetricsAddr   string
	OTelEndpoint  string
	OTelInsecure  bool
	OAuth         OAuthConfig
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
	cfg.RedisURL = getenv("REDIS_URL", "redis://localhost:6379")
	cfg.JWTSignKey = getenv("JWT_SIGN_KEY", "")
	cfg.MetricsAddr = getenv("METRICS_ADDR", "")
	cfg.OTelEndpoint = getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	cfg.OTelInsecure = getenv("OTEL_EXPORTER_OTLP_INSECURE", "true") == "true"

	access := getenv("JWT_ACCESS_TTL", "15m")
	refresh := getenv("JWT_REFRESH_TTL", "720h")

	var err error
	if cfg.JWTAccessTTL, err = time.ParseDuration(access); err != nil {
		return Config{}, fmt.Errorf("parse JWT_ACCESS_TTL: %w", err)
	}
	if cfg.JWTRefreshTTL, err = time.ParseDuration(refresh); err != nil {
		return Config{}, fmt.Errorf("parse JWT_REFRESH_TTL: %w", err)
	}

	// Загрузка OAuth конфигурации
	cfg.OAuth = loadOAuthConfig()

	return cfg, nil
}

func loadOAuthConfig() OAuthConfig {
	var oauth OAuthConfig

	authTokenTTL := getenv("OAUTH_AUTH_TOKEN_TTL", "5m")
	if ttl, err := time.ParseDuration(authTokenTTL); err == nil {
		oauth.AuthTokenTTL = ttl
	} else {
		oauth.AuthTokenTTL = 5 * time.Minute
	}

	sessionTTL := getenv("OAUTH_SESSION_TTL", "24h")
	if ttl, err := time.ParseDuration(sessionTTL); err == nil {
		oauth.SessionTTL = ttl
	} else {
		oauth.SessionTTL = 24 * time.Hour
	}

	verificationCodeTTL := getenv("OAUTH_VERIFICATION_CODE_TTL", "10m")
	if ttl, err := time.ParseDuration(verificationCodeTTL); err == nil {
		oauth.VerificationCodeTTL = ttl
	} else {
		oauth.VerificationCodeTTL = 10 * time.Minute
	}

	oauth.MaxAttemptsPerHour = 10 // TODO: добавить парсинг из env
	oauth.MaxAttemptsPer10Min = 3 // TODO: добавить парсинг из env
	oauth.WebBaseURL = getenv("OAUTH_WEB_BASE_URL", "http://localhost:3000")

	return oauth
}
