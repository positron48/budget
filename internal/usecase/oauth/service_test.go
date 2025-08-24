package oauth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/positron48/budget/internal/domain"
	useauth "github.com/positron48/budget/internal/usecase/auth"
)

// Mock репозиторий для тестирования
type mockOAuthRepo struct {
	authTokens    map[string]domain.OAuthAuthToken
	sessions      map[string]domain.TelegramSession
	logs          []domain.AuthLogEntry
	rateLimits    map[string]int
	accountBlocks map[string]*domain.AccountBlock
}

func newMockOAuthRepo() *mockOAuthRepo {
	return &mockOAuthRepo{
		authTokens:    make(map[string]domain.OAuthAuthToken),
		sessions:      make(map[string]domain.TelegramSession),
		logs:          make([]domain.AuthLogEntry, 0),
		rateLimits:    make(map[string]int),
		accountBlocks: make(map[string]*domain.AccountBlock),
	}
}

func (m *mockOAuthRepo) CreateAuthToken(ctx context.Context, token domain.OAuthAuthToken) error {
	m.authTokens[token.AuthToken] = token
	return nil
}

func (m *mockOAuthRepo) GetAuthTokenByToken(ctx context.Context, authToken string) (domain.OAuthAuthToken, error) {
	token, exists := m.authTokens[authToken]
	if !exists {
		return domain.OAuthAuthToken{}, domain.ErrAuthTokenNotFound
	}
	return token, nil
}

func (m *mockOAuthRepo) UpdateAuthTokenStatus(ctx context.Context, authToken string, status domain.AuthStatus) error {
	token, exists := m.authTokens[authToken]
	if !exists {
		return domain.ErrAuthTokenNotFound
	}
	token.Status = status
	m.authTokens[authToken] = token
	return nil
}

func (m *mockOAuthRepo) CreateTelegramSession(ctx context.Context, session domain.TelegramSession) error {
	m.sessions[session.SessionID] = session
	return nil
}

func (m *mockOAuthRepo) GetTelegramSessionByID(ctx context.Context, sessionID string) (domain.TelegramSession, error) {
	session, exists := m.sessions[sessionID]
	if !exists {
		return domain.TelegramSession{}, domain.ErrSessionNotFound
	}
	return session, nil
}

func (m *mockOAuthRepo) UpdateTelegramSessionLastUsed(ctx context.Context, sessionID string) error {
	session, exists := m.sessions[sessionID]
	if !exists {
		return domain.ErrSessionNotFound
	}
	now := time.Now()
	session.LastUsedAt = &now
	m.sessions[sessionID] = session
	return nil
}

func (m *mockOAuthRepo) RevokeTelegramSession(ctx context.Context, sessionID string) error {
	session, exists := m.sessions[sessionID]
	if !exists {
		return domain.ErrSessionNotFound
	}
	now := time.Now()
	session.RevokedAt = &now
	session.IsActive = false
	m.sessions[sessionID] = session
	return nil
}

func (m *mockOAuthRepo) ListTelegramSessions(ctx context.Context, telegramUserID string) ([]domain.TelegramSession, error) {
	var sessions []domain.TelegramSession
	for _, session := range m.sessions {
		if session.TelegramUserID == telegramUserID && session.IsActive {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (m *mockOAuthRepo) CreateAuthLog(ctx context.Context, log domain.AuthLogEntry) error {
	m.logs = append(m.logs, log)
	return nil
}

func (m *mockOAuthRepo) GetUserByEmail(ctx context.Context, email string) (useauth.User, []useauth.TenantMembership, error) {
	// Возвращаем тестового пользователя
	user := useauth.User{
		ID:    uuid.New().String(),
		Email: email,
		Name:  "Test User",
	}

	memberships := []useauth.TenantMembership{
		{
			TenantID:  uuid.New().String(),
			Role:      "owner",
			IsDefault: true,
		},
	}

	return user, memberships, nil
}

func (m *mockOAuthRepo) GetAuthLogs(ctx context.Context, telegramUserID string, limit, offset int) ([]domain.AuthLogEntry, int, error) {
	var userLogs []domain.AuthLogEntry
	for _, log := range m.logs {
		if log.TelegramUserID == telegramUserID {
			userLogs = append(userLogs, log)
		}
	}

	if offset >= len(userLogs) {
		return []domain.AuthLogEntry{}, len(userLogs), nil
	}

	end := offset + limit
	if end > len(userLogs) {
		end = len(userLogs)
	}

	return userLogs[offset:end], len(userLogs), nil
}

func (m *mockOAuthRepo) CheckRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) (int, error) {
	key := telegramUserID + ":" + action
	return m.rateLimits[key], nil
}

func (m *mockOAuthRepo) IncrementRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) error {
	key := telegramUserID + ":" + action
	m.rateLimits[key]++
	return nil
}

func (m *mockOAuthRepo) CheckAccountBlock(ctx context.Context, telegramUserID string) (*domain.AccountBlock, error) {
	block, exists := m.accountBlocks[telegramUserID]
	if !exists {
		return nil, nil
	}
	return block, nil
}

func (m *mockOAuthRepo) CreateAccountBlock(ctx context.Context, block domain.AccountBlock) error {
	m.accountBlocks[block.TelegramUserID] = &block
	return nil
}

func (m *mockOAuthRepo) CleanupExpiredData(ctx context.Context) error {
	// Простая реализация для тестов
	now := time.Now()
	for token, authToken := range m.authTokens {
		if now.After(authToken.ExpiresAt) {
			delete(m.authTokens, token)
		}
	}
	return nil
}

// Mock кэш для тестирования
type mockOAuthCache struct {
	authTokens        map[string]domain.OAuthAuthToken
	verificationCodes map[string]string
	sessions          map[string]domain.TelegramSession
	rateLimits        map[string]int64
	accountBlocks     map[string]*domain.AccountBlock
}

func newMockOAuthCache() *mockOAuthCache {
	return &mockOAuthCache{
		authTokens:        make(map[string]domain.OAuthAuthToken),
		verificationCodes: make(map[string]string),
		sessions:          make(map[string]domain.TelegramSession),
		rateLimits:        make(map[string]int64),
		accountBlocks:     make(map[string]*domain.AccountBlock),
	}
}

func (m *mockOAuthCache) StoreAuthToken(ctx context.Context, token domain.OAuthAuthToken) error {
	m.authTokens[token.AuthToken] = token
	return nil
}

func (m *mockOAuthCache) GetAuthToken(ctx context.Context, authToken string) (domain.OAuthAuthToken, error) {
	token, exists := m.authTokens[authToken]
	if !exists {
		return domain.OAuthAuthToken{}, domain.ErrAuthTokenNotFound
	}
	return token, nil
}

func (m *mockOAuthCache) UpdateAuthToken(ctx context.Context, authToken string, token domain.OAuthAuthToken) error {
	m.authTokens[authToken] = token
	return nil
}

func (m *mockOAuthCache) DeleteAuthToken(ctx context.Context, authToken string) error {
	delete(m.authTokens, authToken)
	return nil
}

func (m *mockOAuthCache) StoreVerificationCode(ctx context.Context, authToken, code string, ttl time.Duration) error {
	m.verificationCodes[authToken] = code
	return nil
}

func (m *mockOAuthCache) GetVerificationCode(ctx context.Context, authToken string) (string, error) {
	code, exists := m.verificationCodes[authToken]
	if !exists {
		return "", domain.ErrVerificationCodeNotFound
	}
	return code, nil
}

func (m *mockOAuthCache) DeleteVerificationCode(ctx context.Context, authToken string) error {
	delete(m.verificationCodes, authToken)
	return nil
}

func (m *mockOAuthCache) StoreTelegramSession(ctx context.Context, session domain.TelegramSession) error {
	m.sessions[session.SessionID] = session
	return nil
}

func (m *mockOAuthCache) GetTelegramSession(ctx context.Context, sessionID string) (domain.TelegramSession, error) {
	session, exists := m.sessions[sessionID]
	if !exists {
		return domain.TelegramSession{}, domain.ErrSessionNotFound
	}
	return session, nil
}

func (m *mockOAuthCache) DeleteTelegramSession(ctx context.Context, sessionID string) error {
	delete(m.sessions, sessionID)
	return nil
}

func (m *mockOAuthCache) IncrementRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) (int64, error) {
	key := telegramUserID + ":" + action
	m.rateLimits[key]++
	return m.rateLimits[key], nil
}

func (m *mockOAuthCache) GetRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) (int64, error) {
	key := telegramUserID + ":" + action
	return m.rateLimits[key], nil
}

func (m *mockOAuthCache) StoreAccountBlock(ctx context.Context, block domain.AccountBlock) error {
	m.accountBlocks[block.TelegramUserID] = &block
	return nil
}

func (m *mockOAuthCache) GetAccountBlock(ctx context.Context, telegramUserID string) (*domain.AccountBlock, error) {
	block, exists := m.accountBlocks[telegramUserID]
	if !exists {
		return nil, nil
	}
	return block, nil
}

func (m *mockOAuthCache) DeleteAccountBlock(ctx context.Context, telegramUserID string) error {
	delete(m.accountBlocks, telegramUserID)
	return nil
}

func (m *mockOAuthCache) CleanupExpiredKeys(ctx context.Context) error {
	return nil
}

// Mock сервисы
type mockAuthService struct{}

func (m *mockAuthService) Login(ctx context.Context, email, password string) (useauth.User, []useauth.TenantMembership, useauth.TokenPair, error) {
	return useauth.User{}, nil, useauth.TokenPair{}, nil
}

type mockTokenIssuer struct{}

func (m *mockTokenIssuer) Issue(ctx context.Context, userID, tenantID string, accessTTL, refreshTTL time.Duration) (useauth.TokenPair, error) {
	return useauth.TokenPair{
		AccessToken:           "test_access_token",
		RefreshToken:          "test_refresh_token",
		AccessTokenExpiresAt:  time.Now().Add(accessTTL),
		RefreshTokenExpiresAt: time.Now().Add(refreshTTL),
		TokenType:             "Bearer",
	}, nil
}

func TestGenerateAuthLink(t *testing.T) {
	repo := newMockOAuthRepo()
	cache := newMockOAuthCache()
	authService := &mockAuthService{}
	issuer := &mockTokenIssuer{}

	config := domain.OAuthConfig{
		AuthTokenTTL:        5 * time.Minute,
		SessionTTL:          24 * time.Hour,
		VerificationCodeTTL: 10 * time.Minute,
		MaxAttemptsPerHour:  10,
		MaxAttemptsPer10Min: 3,
		WebBaseURL:          "http://localhost:3000",
	}

	service := NewService(repo, cache, authService, issuer, config)

	ctx := context.Background()
	email := "test@example.com"
	telegramUserID := "123456"
	userAgent := "TestBot/1.0"
	ipAddress := "127.0.0.1"

	authURL, authToken, expiresAt, err := service.GenerateAuthLink(ctx, email, telegramUserID, userAgent, ipAddress)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if authURL == "" {
		t.Error("Expected non-empty auth URL")
	}

	if authToken == "" {
		t.Error("Expected non-empty auth token")
	}

	if expiresAt.IsZero() {
		t.Error("Expected non-zero expires at")
	}

	// Проверяем, что токен сохранен в кэше
	_, err = cache.GetAuthToken(ctx, authToken)
	if err != nil {
		t.Errorf("Expected token to be in cache, got error: %v", err)
	}
}

func TestVerifyAuthCode(t *testing.T) {
	repo := newMockOAuthRepo()
	cache := newMockOAuthCache()
	authService := &mockAuthService{}
	issuer := &mockTokenIssuer{}

	config := domain.OAuthConfig{
		AuthTokenTTL:        5 * time.Minute,
		SessionTTL:          24 * time.Hour,
		VerificationCodeTTL: 10 * time.Minute,
		MaxAttemptsPerHour:  10,
		MaxAttemptsPer10Min: 3,
		WebBaseURL:          "http://localhost:3000",
	}

	service := NewService(repo, cache, authService, issuer, config)

	ctx := context.Background()

	// Создаем тестовый токен
	authToken := "test_auth_token"
	verificationCode := "123456"
	telegramUserID := "123456"

	token := domain.OAuthAuthToken{
		ID:               uuid.New().String(),
		AuthToken:        authToken,
		Email:            "test@example.com",
		TelegramUserID:   telegramUserID,
		VerificationCode: verificationCode,
		Status:           domain.AuthStatusPending,
		IPAddress:        "127.0.0.1",
		UserAgent:        "TestBot/1.0",
		CreatedAt:        time.Now(),
		ExpiresAt:        time.Now().Add(5 * time.Minute),
	}

	// Сохраняем токен в кэше и репозитории
	_ = cache.StoreAuthToken(ctx, token)
	_ = repo.CreateAuthToken(ctx, token)
	_ = cache.StoreVerificationCode(ctx, authToken, verificationCode, 10*time.Minute)

	// Тестируем верификацию
	tokenPair, user, memberships, sessionID, err := service.VerifyAuthCode(ctx, authToken, verificationCode, telegramUserID)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if tokenPair.AccessToken == "" {
		t.Error("Expected non-empty access token")
	}

	if user.ID == "" {
		t.Error("Expected non-empty user ID")
	}

	if len(memberships) == 0 {
		t.Error("Expected non-empty memberships")
	}

	if sessionID == "" {
		t.Error("Expected non-empty session ID")
	}

	// Проверяем, что статус токена обновлен
	updatedToken, err := repo.GetAuthTokenByToken(ctx, authToken)
	if err != nil {
		t.Fatalf("Failed to get updated token: %v", err)
	}

	if updatedToken.Status != domain.AuthStatusCompleted {
		t.Errorf("Expected status to be completed, got %s", updatedToken.Status)
	}
}

func TestRateLimitExceeded(t *testing.T) {
	repo := newMockOAuthRepo()
	cache := newMockOAuthCache()
	authService := &mockAuthService{}
	issuer := &mockTokenIssuer{}

	config := domain.OAuthConfig{
		AuthTokenTTL:        5 * time.Minute,
		SessionTTL:          24 * time.Hour,
		VerificationCodeTTL: 10 * time.Minute,
		MaxAttemptsPerHour:  10,
		MaxAttemptsPer10Min: 3,
		WebBaseURL:          "http://localhost:3000",
	}

	service := NewService(repo, cache, authService, issuer, config)

	ctx := context.Background()
	telegramUserID := "123456"

	// Устанавливаем превышение лимита
	cache.rateLimits[telegramUserID+":generate_link"] = 5

	_, _, _, err := service.GenerateAuthLink(ctx, "test@example.com", telegramUserID, "TestBot/1.0", "127.0.0.1")

	if err != ErrRateLimitExceeded {
		t.Errorf("Expected rate limit exceeded error, got %v", err)
	}
}

func TestAccountBlocked(t *testing.T) {
	repo := newMockOAuthRepo()
	cache := newMockOAuthCache()
	authService := &mockAuthService{}
	issuer := &mockTokenIssuer{}

	config := domain.OAuthConfig{
		AuthTokenTTL:        5 * time.Minute,
		SessionTTL:          24 * time.Hour,
		VerificationCodeTTL: 10 * time.Minute,
		MaxAttemptsPerHour:  10,
		MaxAttemptsPer10Min: 3,
		WebBaseURL:          "http://localhost:3000",
	}

	service := NewService(repo, cache, authService, issuer, config)

	ctx := context.Background()
	telegramUserID := "123456"

	// Создаем блокировку аккаунта
	block := domain.AccountBlock{
		ID:             uuid.New().String(),
		TelegramUserID: telegramUserID,
		Reason:         "too_many_failed_attempts",
		BlockedAt:      time.Now(),
		IsActive:       true,
	}

	_ = cache.StoreAccountBlock(ctx, block)

	_, _, _, err := service.GenerateAuthLink(ctx, "test@example.com", telegramUserID, "TestBot/1.0", "127.0.0.1")

	if err != ErrAccountBlocked {
		t.Errorf("Expected account blocked error, got %v", err)
	}
}

func TestInvalidEmail(t *testing.T) {
	repo := newMockOAuthRepo()
	cache := newMockOAuthCache()
	authService := &mockAuthService{}
	issuer := &mockTokenIssuer{}

	config := domain.OAuthConfig{
		AuthTokenTTL:        5 * time.Minute,
		SessionTTL:          24 * time.Hour,
		VerificationCodeTTL: 10 * time.Minute,
		MaxAttemptsPerHour:  10,
		MaxAttemptsPer10Min: 3,
		WebBaseURL:          "http://localhost:3000",
	}

	service := NewService(repo, cache, authService, issuer, config)

	ctx := context.Background()
	telegramUserID := "123456"

	_, _, _, err := service.GenerateAuthLink(ctx, "invalid-email", telegramUserID, "TestBot/1.0", "127.0.0.1")

	if err != ErrInvalidEmail {
		t.Errorf("Expected invalid email error, got %v", err)
	}
}
