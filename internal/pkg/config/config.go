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

	// Обязательные переменные окружения
	cfg.AppEnv = getenv("APP_ENV", "")
	if cfg.AppEnv == "" {
		return Config{}, fmt.Errorf("APP_ENV is required")
	}

	cfg.GRPCAddr = getenv("GRPC_ADDR", "")
	if cfg.GRPCAddr == "" {
		return Config{}, fmt.Errorf("GRPC_ADDR is required")
	}

	cfg.DatabaseURL = getenv("DATABASE_URL", "")
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	cfg.RedisURL = getenv("REDIS_URL", "")
	if cfg.RedisURL == "" {
		return Config{}, fmt.Errorf("REDIS_URL is required")
	}

	cfg.JWTSignKey = getenv("JWT_SIGN_KEY", "")
	if cfg.JWTSignKey == "" {
		return Config{}, fmt.Errorf("JWT_SIGN_KEY is required")
	}

	cfg.MetricsAddr = getenv("METRICS_ADDR", "")
	cfg.OTelEndpoint = getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	cfg.OTelInsecure = getenv("OTEL_EXPORTER_OTLP_INSECURE", "true") == "true"

	access := getenv("JWT_ACCESS_TTL", "")
	if access == "" {
		return Config{}, fmt.Errorf("JWT_ACCESS_TTL is required")
	}

	refresh := getenv("JWT_REFRESH_TTL", "")
	if refresh == "" {
		return Config{}, fmt.Errorf("JWT_REFRESH_TTL is required")
	}

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

	authTokenTTL := getenv("OAUTH_AUTH_TOKEN_TTL", "")
	if authTokenTTL == "" {
		panic("OAUTH_AUTH_TOKEN_TTL is required")
	}
	if ttl, err := time.ParseDuration(authTokenTTL); err != nil {
		panic(fmt.Sprintf("invalid OAUTH_AUTH_TOKEN_TTL: %v", err))
	} else {
		oauth.AuthTokenTTL = ttl
	}

	sessionTTL := getenv("OAUTH_SESSION_TTL", "")
	if sessionTTL == "" {
		panic("OAUTH_SESSION_TTL is required")
	}
	if ttl, err := time.ParseDuration(sessionTTL); err != nil {
		panic(fmt.Sprintf("invalid OAUTH_SESSION_TTL: %v", err))
	} else {
		oauth.SessionTTL = ttl
	}

	verificationCodeTTL := getenv("OAUTH_VERIFICATION_CODE_TTL", "")
	if verificationCodeTTL == "" {
		panic("OAUTH_VERIFICATION_CODE_TTL is required")
	}
	if ttl, err := time.ParseDuration(verificationCodeTTL); err != nil {
		panic(fmt.Sprintf("invalid OAUTH_VERIFICATION_CODE_TTL: %v", err))
	} else {
		oauth.VerificationCodeTTL = ttl
	}

	maxAttemptsPerHour := getenv("OAUTH_MAX_ATTEMPTS_PER_HOUR", "")
	if maxAttemptsPerHour == "" {
		panic("OAUTH_MAX_ATTEMPTS_PER_HOUR is required")
	}
	if attempts, err := fmt.Sscanf(maxAttemptsPerHour, "%d", &oauth.MaxAttemptsPerHour); err != nil || attempts != 1 {
		panic(fmt.Sprintf("invalid OAUTH_MAX_ATTEMPTS_PER_HOUR: %v", err))
	}

	maxAttemptsPer10Min := getenv("OAUTH_MAX_ATTEMPTS_PER_10MIN", "")
	if maxAttemptsPer10Min == "" {
		panic("OAUTH_MAX_ATTEMPTS_PER_10MIN is required")
	}
	if attempts, err := fmt.Sscanf(maxAttemptsPer10Min, "%d", &oauth.MaxAttemptsPer10Min); err != nil || attempts != 1 {
		panic(fmt.Sprintf("invalid OAUTH_MAX_ATTEMPTS_PER_10MIN: %v", err))
	}

	oauth.WebBaseURL = getenv("OAUTH_WEB_BASE_URL", "")
	if oauth.WebBaseURL == "" {
		panic("OAUTH_WEB_BASE_URL is required")
	}

	return oauth
}
