package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	uauth "github.com/positron48/budget/internal/usecase/auth"
)

type JWTIssuer struct {
	signKey []byte
}

func NewJWTIssuer(signKey string) *JWTIssuer {
	return &JWTIssuer{signKey: []byte(signKey)}
}

func (i *JWTIssuer) Issue(ctx context.Context, userID, tenantID string, accessTTL, refreshTTL time.Duration) (uauth.TokenPair, error) {
	now := time.Now()
	accessExp := now.Add(accessTTL)
	refreshExp := now.Add(refreshTTL)

	claims := jwt.MapClaims{
		"sub":       userID,
		"tenant_id": tenantID,
		"iat":       now.Unix(),
		"exp":       accessExp.Unix(),
		"typ":       "access",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	access, err := token.SignedString(i.signKey)
	if err != nil {
		return uauth.TokenPair{}, err
	}

	// Refresh token as opaque random string
	rt := make([]byte, 32)
	if _, err := rand.Read(rt); err != nil {
		return uauth.TokenPair{}, err
	}
	refresh := base64.RawURLEncoding.EncodeToString(rt)

	return uauth.TokenPair{
		AccessToken:           access,
		RefreshToken:          refresh,
		AccessTokenExpiresAt:  accessExp,
		RefreshTokenExpiresAt: refreshExp,
		TokenType:             "Bearer",
	}, nil
}
