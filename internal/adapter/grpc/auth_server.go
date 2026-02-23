//go:build !ignore
// +build !ignore

package grpcadapter

import (
	"context"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	useauth "github.com/positron48/budget/internal/usecase/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type AuthServer struct {
	budgetv1.UnimplementedAuthServiceServer
	svc                 *useauth.Service
	passwordAuthEnabled bool
}

func NewAuthServer(svc *useauth.Service) *AuthServer {
	return &AuthServer{svc: svc, passwordAuthEnabled: true}
}

func NewAuthServerWithPasswordAuth(svc *useauth.Service, enabled bool) *AuthServer {
	return &AuthServer{svc: svc, passwordAuthEnabled: enabled}
}

func (s *AuthServer) Register(ctx context.Context, req *budgetv1.RegisterRequest) (*budgetv1.RegisterResponse, error) {
	if !s.passwordAuthEnabled {
		return nil, status.Error(codes.FailedPrecondition, "password registration is disabled; use GoogleAuth")
	}
	u, t, tp, err := s.svc.Register(ctx, req.GetEmail(), req.GetPassword(), req.GetName(), req.GetLocale(), req.GetTenantName())
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.RegisterResponse{
		Tokens: &budgetv1.TokenPair{
			AccessToken:           tp.AccessToken,
			RefreshToken:          tp.RefreshToken,
			AccessTokenExpiresAt:  timestamppb.New(tp.AccessTokenExpiresAt),
			RefreshTokenExpiresAt: timestamppb.New(tp.RefreshTokenExpiresAt),
			TokenType:             tp.TokenType,
		},
		User:   &budgetv1.User{Id: u.ID, Email: u.Email, Name: u.Name, Locale: u.Locale, EmailVerified: u.EmailVerified},
		Tenant: &budgetv1.Tenant{Id: t.ID, Name: t.Name, DefaultCurrencyCode: t.DefaultCurrencyCode},
	}, nil
}

func (s *AuthServer) Login(ctx context.Context, req *budgetv1.LoginRequest) (*budgetv1.LoginResponse, error) {
	if !s.passwordAuthEnabled {
		return nil, status.Error(codes.FailedPrecondition, "password login is disabled; use GoogleAuth")
	}
	_, memberships, tp, err := s.svc.Login(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, mapError(err)
	}
	ms := make([]*budgetv1.TenantMembership, 0, len(memberships))
	for _, m := range memberships {
		ms = append(ms, &budgetv1.TenantMembership{Tenant: &budgetv1.Tenant{Id: m.TenantID}, Role: mapRole(m.Role), IsDefault: m.IsDefault})
	}
	return &budgetv1.LoginResponse{
		Tokens: &budgetv1.TokenPair{
			AccessToken:           tp.AccessToken,
			RefreshToken:          tp.RefreshToken,
			AccessTokenExpiresAt:  timestamppb.New(tp.AccessTokenExpiresAt),
			RefreshTokenExpiresAt: timestamppb.New(tp.RefreshTokenExpiresAt),
			TokenType:             tp.TokenType,
		},
		Memberships: ms,
	}, nil
}

func (s *AuthServer) GoogleAuth(ctx context.Context, req *budgetv1.GoogleAuthRequest) (*budgetv1.GoogleAuthResponse, error) {
	u, memberships, createdTenant, tp, err := s.svc.GoogleAuth(ctx, req.GetIdToken(), req.GetLocale(), req.GetTenantName())
	if err != nil {
		return nil, mapError(err)
	}
	ms := make([]*budgetv1.TenantMembership, 0, len(memberships))
	for _, m := range memberships {
		ms = append(ms, &budgetv1.TenantMembership{Tenant: &budgetv1.Tenant{Id: m.TenantID}, Role: mapRole(m.Role), IsDefault: m.IsDefault})
	}
	resp := &budgetv1.GoogleAuthResponse{
		Tokens: &budgetv1.TokenPair{
			AccessToken:           tp.AccessToken,
			RefreshToken:          tp.RefreshToken,
			AccessTokenExpiresAt:  timestamppb.New(tp.AccessTokenExpiresAt),
			RefreshTokenExpiresAt: timestamppb.New(tp.RefreshTokenExpiresAt),
			TokenType:             tp.TokenType,
		},
		User:        &budgetv1.User{Id: u.ID, Email: u.Email, Name: u.Name, Locale: u.Locale, EmailVerified: true},
		Memberships: ms,
	}
	if createdTenant != nil {
		resp.Tenant = &budgetv1.Tenant{Id: createdTenant.ID, Name: createdTenant.Name, DefaultCurrencyCode: createdTenant.DefaultCurrencyCode}
	}
	return resp, nil
}

func (s *AuthServer) RefreshToken(ctx context.Context, req *budgetv1.RefreshTokenRequest) (*budgetv1.RefreshTokenResponse, error) {
	tp, err := s.svc.Refresh(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.RefreshTokenResponse{Tokens: &budgetv1.TokenPair{
		AccessToken:           tp.AccessToken,
		RefreshToken:          tp.RefreshToken,
		AccessTokenExpiresAt:  timestamppb.New(tp.AccessTokenExpiresAt),
		RefreshTokenExpiresAt: timestamppb.New(tp.RefreshTokenExpiresAt),
		TokenType:             tp.TokenType,
	}}, nil
}

func mapRole(role string) budgetv1.TenantRole {
	switch role {
	case "owner":
		return budgetv1.TenantRole_TENANT_ROLE_OWNER
	case "admin":
		return budgetv1.TenantRole_TENANT_ROLE_ADMIN
	case "member":
		return budgetv1.TenantRole_TENANT_ROLE_MEMBER
	default:
		return budgetv1.TenantRole_TENANT_ROLE_UNSPECIFIED
	}
}

// mapRoleToDomain was used earlier for conversion; currently not used
