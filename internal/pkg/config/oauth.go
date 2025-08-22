package config

import (
	"time"
)

// OAuthConfig конфигурация OAuth2
type OAuthConfig struct {
	AuthTokenTTL        time.Duration `env:"OAUTH_AUTH_TOKEN_TTL" envDefault:"5m"`
	SessionTTL          time.Duration `env:"OAUTH_SESSION_TTL" envDefault:"24h"`
	VerificationCodeTTL time.Duration `env:"OAUTH_VERIFICATION_CODE_TTL" envDefault:"10m"`
	MaxAttemptsPerHour  int           `env:"OAUTH_MAX_ATTEMPTS_PER_HOUR" envDefault:"10"`
	MaxAttemptsPer10Min int           `env:"OAUTH_MAX_ATTEMPTS_PER_10MIN" envDefault:"3"`
	WebBaseURL          string        `env:"OAUTH_WEB_BASE_URL" envDefault:"http://localhost:3000"`
}

// GetOAuthConfig возвращает конфигурацию OAuth2
func (c *Config) GetOAuthConfig() OAuthConfig {
	return c.OAuth
}
