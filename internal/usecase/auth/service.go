package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) bool
}

type TokenPair struct {
	AccessToken           string
	RefreshToken          string
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
	TokenType             string
}

type TokenIssuer interface {
	Issue(ctx context.Context, userID, tenantID string, accessTTL, refreshTTL time.Duration) (TokenPair, error)
}

type User struct {
	ID            string
	Email         string
	Name          string
	Locale        string
	PasswordHash  string
	EmailVerified bool
}

type Tenant struct {
	ID                  string
	Name                string
	DefaultCurrencyCode string
}

type TenantMembership struct {
	TenantID  string
	Role      string
	IsDefault bool
}

type UserRepo interface {
	CreateWithDefaultTenant(ctx context.Context, email, passwordHash, name, locale, tenantName string) (User, Tenant, error)
	GetByEmail(ctx context.Context, email string) (User, []TenantMembership, error)
}

type RefreshTokenRepo interface {
	Store(ctx context.Context, userID, tenantID, token string, expiresAt time.Time) error
	Rotate(ctx context.Context, oldToken, newToken string, newExpiresAt time.Time) error
	GetByToken(ctx context.Context, token string) (struct {
		UserID    string
		TenantID  string
		ExpiresAt time.Time
		RevokedAt *time.Time
	}, error)
}

type Service struct {
	users      UserRepo
	tokens     RefreshTokenRepo
	hasher     PasswordHasher
	issuer     TokenIssuer
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewService(users UserRepo, tokens RefreshTokenRepo, hasher PasswordHasher, issuer TokenIssuer, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{users: users, tokens: tokens, hasher: hasher, issuer: issuer, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

var ErrInvalidCredentials = errors.New("invalid credentials")

func (s *Service) Register(ctx context.Context, email, password, name, locale, tenantName string) (User, Tenant, TokenPair, error) {
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return User{}, Tenant{}, TokenPair{}, err
	}
	u, t, err := s.users.CreateWithDefaultTenant(ctx, email, hash, name, locale, tenantName)
	if err != nil {
		return User{}, Tenant{}, TokenPair{}, err
	}
	tp, err := s.issuer.Issue(ctx, u.ID, t.ID, s.accessTTL, s.refreshTTL)
	if err != nil {
		return User{}, Tenant{}, TokenPair{}, err
	}
	if err := s.tokens.Store(ctx, u.ID, t.ID, tp.RefreshToken, tp.RefreshTokenExpiresAt); err != nil {
		return User{}, Tenant{}, TokenPair{}, err
	}
	return u, t, tp, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (User, []TenantMembership, TokenPair, error) {
	u, memberships, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return User{}, nil, TokenPair{}, err
	}
	if !s.hasher.Verify(u.PasswordHash, password) {
		return User{}, nil, TokenPair{}, ErrInvalidCredentials
	}
	// выбрать активный tenant: либо default в memberships, либо первый
	tenantID := ""
	for _, m := range memberships {
		if m.IsDefault {
			tenantID = m.TenantID
			break
		}
	}
	if tenantID == "" && len(memberships) > 0 {
		tenantID = memberships[0].TenantID
	}
	
	// Добавляем логирование для отладки
	fmt.Printf("DEBUG: User %s, memberships count: %d\n", u.ID, len(memberships))
	for i, m := range memberships {
		fmt.Printf("DEBUG: Membership %d: TenantID=%s, Role=%s, IsDefault=%v\n", i, m.TenantID, m.Role, m.IsDefault)
	}
	if tenantID == "" {
		// Если tenantID пустой, попробуем получить его из базы данных
		// или создать новый tenant для пользователя
		// Пока что просто логируем проблему
		fmt.Printf("WARNING: Empty tenantID for user %s, memberships: %+v\n", u.ID, memberships)
	} else {
		fmt.Printf("DEBUG: Selected tenantID: %s\n", tenantID)
	}
	
	tp, err := s.issuer.Issue(ctx, u.ID, tenantID, s.accessTTL, s.refreshTTL)
	if err != nil {
		return User{}, nil, TokenPair{}, err
	}
	if err := s.tokens.Store(ctx, u.ID, tenantID, tp.RefreshToken, tp.RefreshTokenExpiresAt); err != nil {
		return User{}, nil, TokenPair{}, err
	}
	return u, memberships, tp, nil
}

// Refresh performs single-use refresh token rotation
func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	row, err := s.tokens.GetByToken(ctx, refreshToken)
	if err != nil {
		return TokenPair{}, ErrInvalidCredentials
	}
	if row.RevokedAt != nil || time.Now().After(row.ExpiresAt) {
		return TokenPair{}, ErrInvalidCredentials
	}
	// issue new pair with tenant_id from refresh token
	tp, err := s.issuer.Issue(ctx, row.UserID, row.TenantID, s.accessTTL, s.refreshTTL)
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.tokens.Rotate(ctx, refreshToken, tp.RefreshToken, tp.RefreshTokenExpiresAt); err != nil {
		return TokenPair{}, err
	}
	return tp, nil
}
