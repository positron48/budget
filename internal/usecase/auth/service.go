package auth

import (
    "context"
    "errors"
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
    ID                   string
    Name                 string
    DefaultCurrencyCode  string
}

type TenantMembership struct {
    TenantID string
    Role     string
    IsDefault bool
}

type UserRepo interface {
    CreateWithDefaultTenant(ctx context.Context, email, passwordHash, name, locale, tenantName string) (User, Tenant, error)
    GetByEmail(ctx context.Context, email string) (User, []TenantMembership, error)
}

type RefreshTokenRepo interface {
    Store(ctx context.Context, userID, token string, expiresAt time.Time) error
    Rotate(ctx context.Context, oldToken, newToken string, newExpiresAt time.Time) error
}

type Service struct {
    users  UserRepo
    tokens RefreshTokenRepo
    hasher PasswordHasher
    issuer TokenIssuer
    accessTTL  time.Duration
    refreshTTL time.Duration
}

func NewService(users UserRepo, tokens RefreshTokenRepo, hasher PasswordHasher, issuer TokenIssuer, accessTTL, refreshTTL time.Duration) *Service {
    return &Service{users: users, tokens: tokens, hasher: hasher, issuer: issuer, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

var ErrInvalidCredentials = errors.New("invalid credentials")

func (s *Service) Register(ctx context.Context, email, password, name, locale, tenantName string) (User, Tenant, TokenPair, error) {
    hash, err := s.hasher.Hash(password)
    if err != nil { return User{}, Tenant{}, TokenPair{}, err }
    u, t, err := s.users.CreateWithDefaultTenant(ctx, email, hash, name, locale, tenantName)
    if err != nil { return User{}, Tenant{}, TokenPair{}, err }
    tp, err := s.issuer.Issue(ctx, u.ID, t.ID, s.accessTTL, s.refreshTTL)
    if err != nil { return User{}, Tenant{}, TokenPair{}, err }
    if err := s.tokens.Store(ctx, u.ID, tp.RefreshToken, tp.RefreshTokenExpiresAt); err != nil { return User{}, Tenant{}, TokenPair{}, err }
    return u, t, tp, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (User, []TenantMembership, TokenPair, error) {
    u, memberships, err := s.users.GetByEmail(ctx, email)
    if err != nil { return User{}, nil, TokenPair{}, err }
    if !s.hasher.Verify(u.PasswordHash, password) { return User{}, nil, TokenPair{}, ErrInvalidCredentials }
    // выбрать активный tenant: либо default в memberships, либо первый
    tenantID := ""
    for _, m := range memberships { if m.IsDefault { tenantID = m.TenantID; break } }
    if tenantID == "" && len(memberships) > 0 { tenantID = memberships[0].TenantID }
    tp, err := s.issuer.Issue(ctx, u.ID, tenantID, s.accessTTL, s.refreshTTL)
    if err != nil { return User{}, nil, TokenPair{}, err }
    if err := s.tokens.Store(ctx, u.ID, tp.RefreshToken, tp.RefreshTokenExpiresAt); err != nil { return User{}, nil, TokenPair{}, err }
    return u, memberships, tp, nil
}


