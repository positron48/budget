package domain

import (
	"errors"
	"time"
)

// OAuthAuthToken представляет временный токен авторизации
type OAuthAuthToken struct {
	ID               string
	AuthToken        string // Хеш токена
	Email            string
	TelegramUserID   string
	VerificationCode string // 6-значный код
	Status           AuthStatus
	IPAddress        string
	UserAgent        string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	CompletedAt      *time.Time
	CancelledAt      *time.Time
}

// AuthStatus статус авторизации
type AuthStatus string

const (
	AuthStatusPending   AuthStatus = "pending"
	AuthStatusCompleted AuthStatus = "completed"
	AuthStatusExpired   AuthStatus = "expired"
	AuthStatusCancelled AuthStatus = "cancelled"
)

// TelegramSession представляет сессию пользователя в Telegram боте
type TelegramSession struct {
	ID               string
	SessionID        string
	UserID           string
	TelegramUserID   string
	TenantID         string
	AccessTokenHash  string
	RefreshTokenHash string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	LastUsedAt       *time.Time
	RevokedAt        *time.Time
	IsActive         bool
}

// AuthLogEntry представляет запись в логе безопасности
type AuthLogEntry struct {
	ID             string
	Email          string
	TelegramUserID string
	IPAddress      string
	UserAgent      string
	Action         ActionType
	Status         LogStatus
	ErrorMessage   string
	AuthTokenID    *string
	SessionID      *string
	CreatedAt      time.Time
}

// ActionType тип действия в логе
type ActionType string

const (
	ActionGenerateLink  ActionType = "generate_link"
	ActionVerifyCode    ActionType = "verify_code"
	ActionCancel        ActionType = "cancel"
	ActionRevokeSession ActionType = "revoke_session"
)

// LogStatus статус операции в логе
type LogStatus string

const (
	LogStatusSuccess LogStatus = "success"
	LogStatusFailed  LogStatus = "failed"
	LogStatusExpired LogStatus = "expired"
)

// RateLimit представляет ограничение скорости запросов
type RateLimit struct {
	ID             string
	TelegramUserID string
	Action         string
	WindowStart    time.Time
	AttemptsCount  int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// AccountBlock представляет блокировку аккаунта
type AccountBlock struct {
	ID             string
	TelegramUserID string
	Email          string
	Reason         string
	BlockedAt      time.Time
	ExpiresAt      *time.Time
	IsActive       bool
}

// Ошибки OAuth2
var (
	ErrAuthTokenNotFound        = errors.New("auth token not found")
	ErrVerificationCodeNotFound = errors.New("verification code not found")
	ErrSessionNotFound          = errors.New("session not found")
)

// OAuthConfig конфигурация OAuth2
type OAuthConfig struct {
	AuthTokenTTL        time.Duration // Время жизни временного токена (5 минут)
	SessionTTL          time.Duration // Время жизни сессии Telegram
	VerificationCodeTTL time.Duration // Время жизни кода подтверждения
	MaxAttemptsPerHour  int           // Максимум попыток в час
	MaxAttemptsPer10Min int           // Максимум попыток за 10 минут
	WebBaseURL          string        // Базовый URL веб-интерфейса
}
