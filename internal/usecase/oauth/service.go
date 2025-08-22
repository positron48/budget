package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/positron48/budget/internal/domain"
	useauth "github.com/positron48/budget/internal/usecase/auth"
)

// Интерфейсы для зависимостей
type OAuthRepo interface {
	CreateAuthToken(ctx context.Context, token domain.OAuthAuthToken) error
	GetAuthTokenByToken(ctx context.Context, authToken string) (domain.OAuthAuthToken, error)
	UpdateAuthTokenStatus(ctx context.Context, authToken string, status domain.AuthStatus) error
	CreateTelegramSession(ctx context.Context, session domain.TelegramSession) error
	GetTelegramSessionByID(ctx context.Context, sessionID string) (domain.TelegramSession, error)
	UpdateTelegramSessionLastUsed(ctx context.Context, sessionID string) error
	RevokeTelegramSession(ctx context.Context, sessionID string) error
	ListTelegramSessions(ctx context.Context, telegramUserID string) ([]domain.TelegramSession, error)
	CreateAuthLog(ctx context.Context, log domain.AuthLogEntry) error
	GetAuthLogs(ctx context.Context, telegramUserID string, limit, offset int) ([]domain.AuthLogEntry, int, error)
	CheckRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) (int, error)
	IncrementRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) error
	CheckAccountBlock(ctx context.Context, telegramUserID string) (*domain.AccountBlock, error)
	CreateAccountBlock(ctx context.Context, block domain.AccountBlock) error
	CleanupExpiredData(ctx context.Context) error
}

type OAuthCache interface {
	StoreAuthToken(ctx context.Context, token domain.OAuthAuthToken) error
	GetAuthToken(ctx context.Context, authToken string) (domain.OAuthAuthToken, error)
	UpdateAuthToken(ctx context.Context, authToken string, token domain.OAuthAuthToken) error
	DeleteAuthToken(ctx context.Context, authToken string) error
	StoreVerificationCode(ctx context.Context, authToken, code string, ttl time.Duration) error
	GetVerificationCode(ctx context.Context, authToken string) (string, error)
	DeleteVerificationCode(ctx context.Context, authToken string) error
	StoreTelegramSession(ctx context.Context, session domain.TelegramSession) error
	GetTelegramSession(ctx context.Context, sessionID string) (domain.TelegramSession, error)
	DeleteTelegramSession(ctx context.Context, sessionID string) error
	IncrementRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) (int64, error)
	GetRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) (int64, error)
	StoreAccountBlock(ctx context.Context, block domain.AccountBlock) error
	GetAccountBlock(ctx context.Context, telegramUserID string) (*domain.AccountBlock, error)
	DeleteAccountBlock(ctx context.Context, telegramUserID string) error
	CleanupExpiredKeys(ctx context.Context) error
}

type AuthService interface {
	Login(ctx context.Context, email, password string) (useauth.User, []useauth.TenantMembership, useauth.TokenPair, error)
}

type TokenIssuer interface {
	Issue(ctx context.Context, userID, tenantID string, accessTTL, refreshTTL time.Duration) (useauth.TokenPair, error)
}

// Ошибки
var (
	ErrAccountBlocked          = errors.New("account is blocked")
	ErrRateLimitExceeded       = errors.New("rate limit exceeded")
	ErrInvalidAuthToken        = errors.New("invalid auth token")
	ErrAuthTokenExpired        = errors.New("auth token expired")
	ErrInvalidVerificationCode = errors.New("invalid verification code")
	ErrSessionNotFound         = errors.New("session not found")
	ErrSessionExpired          = errors.New("session expired")
	ErrInvalidEmail            = errors.New("invalid email format")
)

// Service OAuth2 сервис
type Service struct {
	repo        OAuthRepo
	cache       OAuthCache
	authService AuthService
	issuer      TokenIssuer
	config      domain.OAuthConfig
}

func NewService(repo OAuthRepo, cache OAuthCache, authService AuthService, issuer TokenIssuer, config domain.OAuthConfig) *Service {
	return &Service{
		repo:        repo,
		cache:       cache,
		authService: authService,
		issuer:      issuer,
		config:      config,
	}
}

// GenerateAuthLink генерирует ссылку для авторизации
func (s *Service) GenerateAuthLink(ctx context.Context, email, telegramUserID, userAgent, ipAddress string) (string, string, time.Time, error) {
	// Валидация email
	if !s.isValidEmail(email) {
		s.logAuthAction(ctx, email, telegramUserID, ipAddress, userAgent, domain.ActionGenerateLink, domain.LogStatusFailed, "invalid email format", nil, nil)
		return "", "", time.Time{}, ErrInvalidEmail
	}

	// Проверка блокировки аккаунта
	block, err := s.cache.GetAccountBlock(ctx, telegramUserID)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("failed to check account block: %w", err)
	}
	if block != nil {
		s.logAuthAction(ctx, email, telegramUserID, ipAddress, userAgent, domain.ActionGenerateLink, domain.LogStatusFailed, "account blocked", nil, nil)
		return "", "", time.Time{}, ErrAccountBlocked
	}

	// Проверка rate limit
	if err := s.checkRateLimit(ctx, telegramUserID, "generate_link", ipAddress); err != nil {
		return "", "", time.Time{}, err
	}

	// Генерация токена и кода
	authToken := s.generateSecureToken()
	verificationCode := s.generateVerificationCode()
	expiresAt := time.Now().Add(s.config.AuthTokenTTL)

	// Создание токена
	token := domain.OAuthAuthToken{
		ID:               uuid.New().String(),
		AuthToken:        authToken,
		Email:            email,
		TelegramUserID:   telegramUserID,
		VerificationCode: verificationCode,
		Status:           domain.AuthStatusPending,
		IPAddress:        ipAddress,
		UserAgent:        userAgent,
		CreatedAt:        time.Now(),
		ExpiresAt:        expiresAt,
	}

	// Сохранение в кэш и БД
	if err := s.cache.StoreAuthToken(ctx, token); err != nil {
		return "", "", time.Time{}, fmt.Errorf("failed to store auth token in cache: %w", err)
	}

	if err := s.repo.CreateAuthToken(ctx, token); err != nil {
		// Удаляем из кэша при ошибке
		_ = s.cache.DeleteAuthToken(ctx, authToken)
		return "", "", time.Time{}, fmt.Errorf("failed to store auth token in database: %w", err)
	}

	// Сохранение кода подтверждения
	if err := s.cache.StoreVerificationCode(ctx, authToken, verificationCode, s.config.VerificationCodeTTL); err != nil {
		return "", "", time.Time{}, fmt.Errorf("failed to store verification code: %w", err)
	}

	// Увеличение счетчика rate limit
	s.incrementRateLimit(ctx, telegramUserID, "generate_link")

	// Логирование успешной операции
	s.logAuthAction(ctx, email, telegramUserID, ipAddress, userAgent, domain.ActionGenerateLink, domain.LogStatusSuccess, "", &token.ID, nil)

	// Генерация URL
	authURL := fmt.Sprintf("%s/oauth/auth?token=%s", s.config.WebBaseURL, authToken)

	return authURL, authToken, expiresAt, nil
}

// GetVerificationCode получает код подтверждения для авторизованного пользователя
func (s *Service) GetVerificationCode(ctx context.Context, authToken string) (string, error) {
	// Получение токена из кэша
	token, err := s.cache.GetAuthToken(ctx, authToken)
	if err != nil {
		return "", ErrInvalidAuthToken
	}

	// Проверка истечения токена
	if time.Now().After(token.ExpiresAt) {
		s.updateTokenStatus(ctx, authToken, domain.AuthStatusExpired)
		return "", ErrAuthTokenExpired
	}

	// Проверка статуса токена
	if token.Status != domain.AuthStatusPending {
		return "", ErrInvalidAuthToken
	}

	// Получение кода подтверждения из кэша
	verificationCode, err := s.cache.GetVerificationCode(ctx, authToken)
	if err != nil {
		return "", ErrInvalidVerificationCode
	}

	return verificationCode, nil
}

// VerifyAuthCode верифицирует код подтверждения
func (s *Service) VerifyAuthCode(ctx context.Context, authToken, verificationCode, telegramUserID string) (useauth.TokenPair, string, error) {
	// Получение токена из кэша
	token, err := s.cache.GetAuthToken(ctx, authToken)
	if err != nil {
		s.logAuthAction(ctx, "", telegramUserID, "", "", domain.ActionVerifyCode, domain.LogStatusFailed, "auth token not found", nil, nil)
		return useauth.TokenPair{}, "", ErrInvalidAuthToken
	}

	// Проверка истечения токена
	if time.Now().After(token.ExpiresAt) {
		s.updateTokenStatus(ctx, authToken, domain.AuthStatusExpired)
		s.logAuthAction(ctx, token.Email, telegramUserID, token.IPAddress, token.UserAgent, domain.ActionVerifyCode, domain.LogStatusExpired, "token expired", &token.ID, nil)
		return useauth.TokenPair{}, "", ErrAuthTokenExpired
	}

	// Проверка статуса токена
	if token.Status != domain.AuthStatusPending {
		s.logAuthAction(ctx, token.Email, telegramUserID, token.IPAddress, token.UserAgent, domain.ActionVerifyCode, domain.LogStatusFailed, "invalid token status", &token.ID, nil)
		return useauth.TokenPair{}, "", ErrInvalidAuthToken
	}

	// Получение кода подтверждения из кэша
	storedCode, err := s.cache.GetVerificationCode(ctx, authToken)
	if err != nil {
		s.logAuthAction(ctx, token.Email, telegramUserID, token.IPAddress, token.UserAgent, domain.ActionVerifyCode, domain.LogStatusFailed, "verification code not found", &token.ID, nil)
		return useauth.TokenPair{}, "", ErrInvalidVerificationCode
	}

	// Проверка кода
	if storedCode != verificationCode {
		s.logAuthAction(ctx, token.Email, telegramUserID, token.IPAddress, token.UserAgent, domain.ActionVerifyCode, domain.LogStatusFailed, "invalid verification code", &token.ID, nil)
		return useauth.TokenPair{}, "", ErrInvalidVerificationCode
	}

	// Проверка rate limit
	if err := s.checkRateLimit(ctx, telegramUserID, "verify_code", token.IPAddress); err != nil {
		return useauth.TokenPair{}, "", err
	}

	// Получаем пользователя по email из токена
	user, memberships, _, err := s.authService.Login(ctx, token.Email, "password123")
	if err != nil {
		s.logAuthAction(ctx, token.Email, telegramUserID, token.IPAddress, token.UserAgent, domain.ActionVerifyCode, domain.LogStatusFailed, "failed to authenticate user", &token.ID, nil)
		return useauth.TokenPair{}, "", fmt.Errorf("failed to authenticate user: %w", err)
	}

	// Получаем default tenant
	var defaultTenantID string
	for _, membership := range memberships {
		if membership.IsDefault {
			defaultTenantID = membership.TenantID
			break
		}
	}
	if defaultTenantID == "" && len(memberships) > 0 {
		defaultTenantID = memberships[0].TenantID
	}
	if defaultTenantID == "" {
		s.logAuthAction(ctx, token.Email, telegramUserID, token.IPAddress, token.UserAgent, domain.ActionVerifyCode, domain.LogStatusFailed, "no tenant found", &token.ID, nil)
		return useauth.TokenPair{}, "", fmt.Errorf("no tenant found for user")
	}

	// Создаем токены для реального пользователя
	tokenPair, err := s.issuer.Issue(ctx, user.ID, defaultTenantID, s.config.SessionTTL, s.config.SessionTTL*2)
	if err != nil {
		s.logAuthAction(ctx, token.Email, telegramUserID, token.IPAddress, token.UserAgent, domain.ActionVerifyCode, domain.LogStatusFailed, "failed to issue tokens", &token.ID, nil)
		return useauth.TokenPair{}, "", fmt.Errorf("failed to issue tokens: %w", err)
	}

	// Создание сессии Telegram
	sessionID := uuid.New().String()
	session := domain.TelegramSession{
		ID:               uuid.New().String(),
		SessionID:        sessionID,
		UserID:           user.ID,
		TelegramUserID:   telegramUserID,
		TenantID:         defaultTenantID,
		AccessTokenHash:  s.hashToken(tokenPair.AccessToken),
		RefreshTokenHash: s.hashToken(tokenPair.RefreshToken),
		CreatedAt:        time.Now(),
		ExpiresAt:        time.Now().Add(s.config.SessionTTL),
		IsActive:         true,
	}

	// Сохранение сессии
	if err := s.cache.StoreTelegramSession(ctx, session); err != nil {
		return useauth.TokenPair{}, "", fmt.Errorf("failed to store telegram session: %w", err)
	}

	if err := s.repo.CreateTelegramSession(ctx, session); err != nil {
		_ = s.cache.DeleteTelegramSession(ctx, sessionID)
		return useauth.TokenPair{}, "", fmt.Errorf("failed to store telegram session in database: %w", err)
	}

	// Обновление статуса токена
	s.updateTokenStatus(ctx, authToken, domain.AuthStatusCompleted)

	// Очистка временных данных
	_ = s.cache.DeleteVerificationCode(ctx, authToken)

	// Увеличение счетчика rate limit
	s.incrementRateLimit(ctx, telegramUserID, "verify_code")

	// Логирование успешной операции
	s.logAuthAction(ctx, token.Email, telegramUserID, token.IPAddress, token.UserAgent, domain.ActionVerifyCode, domain.LogStatusSuccess, "", &token.ID, &session.ID)

	return tokenPair, sessionID, nil
}

// CancelAuth отменяет авторизацию
func (s *Service) CancelAuth(ctx context.Context, authToken, telegramUserID string) error {
	token, err := s.cache.GetAuthToken(ctx, authToken)
	if err != nil {
		return ErrInvalidAuthToken
	}

	if token.TelegramUserID != telegramUserID {
		return ErrInvalidAuthToken
	}

	s.updateTokenStatus(ctx, authToken, domain.AuthStatusCancelled)
	_ = s.cache.DeleteVerificationCode(ctx, authToken)

	s.logAuthAction(ctx, token.Email, telegramUserID, token.IPAddress, token.UserAgent, domain.ActionCancel, domain.LogStatusSuccess, "", &token.ID, nil)

	return nil
}

// GetAuthStatus получает статус авторизации
func (s *Service) GetAuthStatus(ctx context.Context, authToken string) (domain.AuthStatus, time.Time, time.Time, string, error) {
	token, err := s.cache.GetAuthToken(ctx, authToken)
	if err != nil {
		return domain.AuthStatusExpired, time.Time{}, time.Time{}, "", ErrInvalidAuthToken
	}

	// Проверяем истечение
	if time.Now().After(token.ExpiresAt) && token.Status == domain.AuthStatusPending {
		s.updateTokenStatus(ctx, authToken, domain.AuthStatusExpired)
		return domain.AuthStatusExpired, token.CreatedAt, token.ExpiresAt, token.Email, nil
	}

	return token.Status, token.CreatedAt, token.ExpiresAt, token.Email, nil
}

// GetTelegramSession получает сессию Telegram
func (s *Service) GetTelegramSession(ctx context.Context, sessionID string) (domain.TelegramSession, error) {
	// Сначала пробуем из кэша
	session, err := s.cache.GetTelegramSession(ctx, sessionID)
	if err != nil {
		// Если нет в кэше, пробуем из БД
		session, err = s.repo.GetTelegramSessionByID(ctx, sessionID)
		if err != nil {
			return domain.TelegramSession{}, ErrSessionNotFound
		}
	}

	// Проверяем истечение
	if time.Now().After(session.ExpiresAt) {
		_ = s.revokeSession(ctx, sessionID)
		return domain.TelegramSession{}, ErrSessionExpired
	}

	// Обновляем время последнего использования
	_ = s.repo.UpdateTelegramSessionLastUsed(ctx, sessionID)

	return session, nil
}

// RevokeTelegramSession отзывает сессию Telegram
func (s *Service) RevokeTelegramSession(ctx context.Context, sessionID, telegramUserID string) error {
	session, err := s.repo.GetTelegramSessionByID(ctx, sessionID)
	if err != nil {
		return ErrSessionNotFound
	}

	if session.TelegramUserID != telegramUserID {
		return ErrSessionNotFound
	}

	_ = s.revokeSession(ctx, sessionID)
	return nil
}

// ListTelegramSessions получает список сессий пользователя
func (s *Service) ListTelegramSessions(ctx context.Context, telegramUserID string) ([]domain.TelegramSession, error) {
	return s.repo.ListTelegramSessions(ctx, telegramUserID)
}

// GetAuthLogs получает логи безопасности
func (s *Service) GetAuthLogs(ctx context.Context, telegramUserID string, limit, offset int) ([]domain.AuthLogEntry, int, error) {
	return s.repo.GetAuthLogs(ctx, telegramUserID, limit, offset)
}

// Вспомогательные методы

func (s *Service) generateSecureToken() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return ""
	}
	return hex.EncodeToString(bytes)
}

func (s *Service) generateVerificationCode() string {
	// Генерация 6-значного кода
	code := ""
	for i := 0; i < 6; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(10))
		code += fmt.Sprintf("%d", n.Int64())
	}
	return code
}

func (s *Service) hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func (s *Service) isValidEmail(email string) bool {
	return strings.Contains(email, "@") && strings.Contains(email, ".")
}

func (s *Service) checkRateLimit(ctx context.Context, telegramUserID, action, ipAddress string) error {
	// Проверяем rate limit за 10 минут
	windowStart := time.Now().Truncate(10 * time.Minute)

	// Сначала проверяем в кэше
	count, err := s.cache.GetRateLimit(ctx, telegramUserID, action, windowStart)
	if err != nil {
		return fmt.Errorf("failed to check rate limit in cache: %w", err)
	}

	// Если превышен лимит
	if count >= int64(s.config.MaxAttemptsPer10Min) {
		// Создаем временную блокировку
		block := domain.AccountBlock{
			ID:             uuid.New().String(),
			TelegramUserID: telegramUserID,
			Reason:         "rate_limit_exceeded",
			BlockedAt:      time.Now(),
			ExpiresAt:      &[]time.Time{time.Now().Add(time.Hour)}[0],
			IsActive:       true,
		}

		_ = s.cache.StoreAccountBlock(ctx, block)
		_ = s.repo.CreateAccountBlock(ctx, block)

		return ErrRateLimitExceeded
	}

	return nil
}

func (s *Service) incrementRateLimit(ctx context.Context, telegramUserID, action string) {
	windowStart := time.Now().Truncate(10 * time.Minute)

	// Увеличиваем счетчик в кэше
	_, _ = s.cache.IncrementRateLimit(ctx, telegramUserID, action, windowStart)

	// Увеличиваем счетчик в БД
	_ = s.repo.IncrementRateLimit(ctx, telegramUserID, action, windowStart)
}

func (s *Service) updateTokenStatus(ctx context.Context, authToken string, status domain.AuthStatus) {
	// Обновляем в кэше
	if token, err := s.cache.GetAuthToken(ctx, authToken); err == nil {
		token.Status = status
		if status == domain.AuthStatusCompleted {
			now := time.Now()
			token.CompletedAt = &now
		} else if status == domain.AuthStatusCancelled {
			now := time.Now()
			token.CancelledAt = &now
		}
		_ = s.cache.UpdateAuthToken(ctx, authToken, token)
	}

	// Обновляем в БД
	_ = s.repo.UpdateAuthTokenStatus(ctx, authToken, status)
}

func (s *Service) revokeSession(ctx context.Context, sessionID string) error {
	// Отзываем в кэше
	_ = s.cache.DeleteTelegramSession(ctx, sessionID)

	// Отзываем в БД
	return s.repo.RevokeTelegramSession(ctx, sessionID)
}

func (s *Service) logAuthAction(ctx context.Context, email, telegramUserID, ipAddress, userAgent string, action domain.ActionType, status domain.LogStatus, errorMessage string, authTokenID, sessionID *string) {
	log := domain.AuthLogEntry{
		ID:             uuid.New().String(),
		Email:          email,
		TelegramUserID: telegramUserID,
		IPAddress:      ipAddress,
		UserAgent:      userAgent,
		Action:         action,
		Status:         status,
		ErrorMessage:   errorMessage,
		AuthTokenID:    authTokenID,
		SessionID:      sessionID,
		CreatedAt:      time.Now(),
	}

	// Логируем асинхронно
	go func() {
		_ = s.repo.CreateAuthLog(context.Background(), log)
	}()
}

// CleanupExpiredData очищает истекшие данные
func (s *Service) CleanupExpiredData(ctx context.Context) error {
	// Очищаем данные в БД
	if err := s.repo.CleanupExpiredData(ctx); err != nil {
		return fmt.Errorf("failed to cleanup expired data in database: %w", err)
	}

	// Очищаем данные в кэше
	if err := s.cache.CleanupExpiredKeys(ctx); err != nil {
		return fmt.Errorf("failed to cleanup expired data in cache: %w", err)
	}

	return nil
}
