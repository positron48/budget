package grpcadapter

import (
	"context"
	"errors"
	"fmt"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/domain"
	useoauth "github.com/positron48/budget/internal/usecase/oauth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type OAuthServer struct {
	budgetv1.UnimplementedOAuthServiceServer
	svc *useoauth.Service
}

func NewOAuthServer(svc *useoauth.Service) *OAuthServer {
	return &OAuthServer{svc: svc}
}

func (s *OAuthServer) GenerateAuthLink(ctx context.Context, req *budgetv1.GenerateAuthLinkRequest) (*budgetv1.GenerateAuthLinkResponse, error) {
	// Получение IP адреса и User-Agent из контекста
	ipAddress, userAgent := s.extractClientInfo(ctx)

	// Валидация запроса
	if req.GetEmail() == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if req.GetTelegramUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "telegram_user_id is required")
	}

	// Генерация ссылки авторизации
	authURL, authToken, expiresAt, err := s.svc.GenerateAuthLink(ctx, req.GetEmail(), req.GetTelegramUserId(), userAgent, ipAddress)
	if err != nil {
		return nil, s.mapOAuthError(err)
	}

	return &budgetv1.GenerateAuthLinkResponse{
		AuthUrl:   authURL,
		AuthToken: authToken,
		ExpiresAt: timestamppb.New(expiresAt),
	}, nil
}

func (s *OAuthServer) GetVerificationCode(ctx context.Context, req *budgetv1.GetVerificationCodeRequest) (*budgetv1.GetVerificationCodeResponse, error) {
	// Валидация запроса
	if req.GetAuthToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "auth_token is required")
	}

	// Получение кода подтверждения
	verificationCode, err := s.svc.GetVerificationCode(ctx, req.GetAuthToken())
	if err != nil {
		return nil, s.mapOAuthError(err)
	}

	return &budgetv1.GetVerificationCodeResponse{
		VerificationCode: verificationCode,
		Message:          "Verification code generated successfully",
	}, nil
}

func (s *OAuthServer) VerifyAuthCode(ctx context.Context, req *budgetv1.VerifyAuthCodeRequest) (*budgetv1.VerifyAuthCodeResponse, error) {
	// Валидация запроса
	if req.GetAuthToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "auth_token is required")
	}
	if req.GetVerificationCode() == "" {
		return nil, status.Error(codes.InvalidArgument, "verification_code is required")
	}
	if req.GetTelegramUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "telegram_user_id is required")
	}

	// Верификация кода
	tokenPair, sessionID, err := s.svc.VerifyAuthCode(ctx, req.GetAuthToken(), req.GetVerificationCode(), req.GetTelegramUserId())
	if err != nil {
		return nil, s.mapOAuthError(err)
	}

	// Преобразование в proto формат
	protoTokenPair := &budgetv1.TokenPair{
		AccessToken:           tokenPair.AccessToken,
		RefreshToken:          tokenPair.RefreshToken,
		AccessTokenExpiresAt:  timestamppb.New(tokenPair.AccessTokenExpiresAt),
		RefreshTokenExpiresAt: timestamppb.New(tokenPair.RefreshTokenExpiresAt),
		TokenType:             tokenPair.TokenType,
	}

	// TODO: Получить реальные данные пользователя и tenant из AuthService
	// Пока возвращаем заглушки, но с правильными UUID из токенов
	user := &budgetv1.User{
		Id:            "00000000-0000-0000-0000-000000000001", // Будет заменено на реальный ID
		Email:         "test@example.com",
		Name:          "Test User",
		Locale:        "en",
		EmailVerified: true,
	}

	tenant := &budgetv1.Tenant{
		Id:                  "00000000-0000-0000-0000-000000000002", // Будет заменено на реальный ID
		Name:                "Test Tenant",
		DefaultCurrencyCode: "USD",
	}

	memberships := []*budgetv1.TenantMembership{
		{
			Tenant:    tenant,
			Role:      budgetv1.TenantRole_TENANT_ROLE_OWNER,
			IsDefault: true,
		},
	}

	return &budgetv1.VerifyAuthCodeResponse{
		Tokens:      protoTokenPair,
		User:        user,
		Memberships: memberships,
		SessionId:   sessionID,
	}, nil
}

func (s *OAuthServer) CancelAuth(ctx context.Context, req *budgetv1.CancelAuthRequest) (*budgetv1.CancelAuthResponse, error) {
	// Валидация запроса
	if req.GetAuthToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "auth_token is required")
	}
	if req.GetTelegramUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "telegram_user_id is required")
	}

	// Отмена авторизации
	err := s.svc.CancelAuth(ctx, req.GetAuthToken(), req.GetTelegramUserId())
	if err != nil {
		return nil, s.mapOAuthError(err)
	}

	return &budgetv1.CancelAuthResponse{}, nil
}

func (s *OAuthServer) GetAuthStatus(ctx context.Context, req *budgetv1.GetAuthStatusRequest) (*budgetv1.GetAuthStatusResponse, error) {
	// Валидация запроса
	if req.GetAuthToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "auth_token is required")
	}

	// Получение статуса авторизации
	status, createdAt, expiresAt, email, err := s.svc.GetAuthStatus(ctx, req.GetAuthToken())
	if err != nil {
		return nil, s.mapOAuthError(err)
	}

	// Преобразование статуса в proto формат
	var protoStatus budgetv1.GetAuthStatusResponse_Status
	switch status {
	case domain.AuthStatusPending:
		protoStatus = budgetv1.GetAuthStatusResponse_STATUS_PENDING
	case domain.AuthStatusCompleted:
		protoStatus = budgetv1.GetAuthStatusResponse_STATUS_COMPLETED
	case domain.AuthStatusExpired:
		protoStatus = budgetv1.GetAuthStatusResponse_STATUS_EXPIRED
	case domain.AuthStatusCancelled:
		protoStatus = budgetv1.GetAuthStatusResponse_STATUS_CANCELLED
	default:
		protoStatus = budgetv1.GetAuthStatusResponse_STATUS_UNSPECIFIED
	}

	return &budgetv1.GetAuthStatusResponse{
		Status:    protoStatus,
		CreatedAt: timestamppb.New(createdAt),
		ExpiresAt: timestamppb.New(expiresAt),
		Email:     email,
	}, nil
}

func (s *OAuthServer) GetTelegramSession(ctx context.Context, req *budgetv1.GetTelegramSessionRequest) (*budgetv1.GetTelegramSessionResponse, error) {
	// Валидация запроса
	if req.GetSessionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	// Получение сессии
	session, err := s.svc.GetTelegramSession(ctx, req.GetSessionId())
	if err != nil {
		return nil, s.mapOAuthError(err)
	}

	// Преобразование в proto формат
	protoSession := &budgetv1.TelegramSession{
		SessionId:      session.SessionID,
		UserId:         session.UserID,
		TelegramUserId: session.TelegramUserID,
		TenantId:       session.TenantID,
		CreatedAt:      timestamppb.New(session.CreatedAt),
		ExpiresAt:      timestamppb.New(session.ExpiresAt),
		IsActive:       session.IsActive,
	}

	// TODO: Получить реальные данные пользователя и tenant
	user := &budgetv1.User{
		Id:            session.UserID,
		Email:         "user@example.com",
		Name:          "User",
		Locale:        "en",
		EmailVerified: true,
	}

	tenant := &budgetv1.Tenant{
		Id:                  session.TenantID,
		Name:                "Tenant",
		DefaultCurrencyCode: "USD",
	}

	return &budgetv1.GetTelegramSessionResponse{
		Session: protoSession,
		User:    user,
		Tenant:  tenant,
	}, nil
}

func (s *OAuthServer) RevokeTelegramSession(ctx context.Context, req *budgetv1.RevokeTelegramSessionRequest) (*budgetv1.RevokeTelegramSessionResponse, error) {
	// Валидация запроса
	if req.GetSessionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.GetTelegramUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "telegram_user_id is required")
	}

	// Отзыв сессии
	err := s.svc.RevokeTelegramSession(ctx, req.GetSessionId(), req.GetTelegramUserId())
	if err != nil {
		return nil, s.mapOAuthError(err)
	}

	return &budgetv1.RevokeTelegramSessionResponse{}, nil
}

func (s *OAuthServer) ListTelegramSessions(ctx context.Context, req *budgetv1.ListTelegramSessionsRequest) (*budgetv1.ListTelegramSessionsResponse, error) {
	// Валидация запроса
	if req.GetTelegramUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "telegram_user_id is required")
	}

	// Получение списка сессий
	sessions, err := s.svc.ListTelegramSessions(ctx, req.GetTelegramUserId())
	if err != nil {
		return nil, s.mapOAuthError(err)
	}

	// Преобразование в proto формат
	protoSessions := make([]*budgetv1.TelegramSession, 0, len(sessions))
	for _, session := range sessions {
		protoSession := &budgetv1.TelegramSession{
			SessionId:      session.SessionID,
			UserId:         session.UserID,
			TelegramUserId: session.TelegramUserID,
			TenantId:       session.TenantID,
			CreatedAt:      timestamppb.New(session.CreatedAt),
			ExpiresAt:      timestamppb.New(session.ExpiresAt),
			IsActive:       session.IsActive,
		}
		protoSessions = append(protoSessions, protoSession)
	}

	return &budgetv1.ListTelegramSessionsResponse{
		Sessions: protoSessions,
	}, nil
}

func (s *OAuthServer) GetAuthLogs(ctx context.Context, req *budgetv1.GetAuthLogsRequest) (*budgetv1.GetAuthLogsResponse, error) {
	// Валидация запроса
	if req.GetTelegramUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "telegram_user_id is required")
	}

	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 50 // Значение по умолчанию
	}
	if limit > 100 {
		limit = 100 // Максимальное значение
	}

	offset := int(req.GetOffset())
	if offset < 0 {
		offset = 0
	}

	// Получение логов
	logs, totalCount, err := s.svc.GetAuthLogs(ctx, req.GetTelegramUserId(), limit, offset)
	if err != nil {
		return nil, s.mapOAuthError(err)
	}

	// Преобразование в proto формат
	protoLogs := make([]*budgetv1.AuthLogEntry, 0, len(logs))
	for _, log := range logs {
		protoLog := &budgetv1.AuthLogEntry{
			Id:             log.ID,
			Email:          log.Email,
			TelegramUserId: log.TelegramUserID,
			IpAddress:      log.IPAddress,
			UserAgent:      log.UserAgent,
			Action:         string(log.Action),
			Status:         string(log.Status),
			ErrorMessage:   log.ErrorMessage,
			CreatedAt:      timestamppb.New(log.CreatedAt),
		}
		protoLogs = append(protoLogs, protoLog)
	}

	return &budgetv1.GetAuthLogsResponse{
		Logs:       protoLogs,
		TotalCount: int32(totalCount),
	}, nil
}

// Вспомогательные методы

func (s *OAuthServer) extractClientInfo(ctx context.Context) (string, string) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", ""
	}

	// Получение IP адреса
	ipAddress := ""
	if xff := md.Get("x-forwarded-for"); len(xff) > 0 {
		ipAddress = xff[0]
	} else if xri := md.Get("x-real-ip"); len(xri) > 0 {
		ipAddress = xri[0]
	}

	// Получение User-Agent
	userAgent := ""
	if ua := md.Get("user-agent"); len(ua) > 0 {
		userAgent = ua[0]
	}

	return ipAddress, userAgent
}

func (s *OAuthServer) mapOAuthError(err error) error {
	switch {
	case errors.Is(err, useoauth.ErrAccountBlocked):
		return status.Error(codes.PermissionDenied, "account is blocked")
	case errors.Is(err, useoauth.ErrRateLimitExceeded):
		return status.Error(codes.ResourceExhausted, "rate limit exceeded")
	case errors.Is(err, useoauth.ErrInvalidAuthToken):
		return status.Error(codes.InvalidArgument, "invalid auth token")
	case errors.Is(err, useoauth.ErrAuthTokenExpired):
		return status.Error(codes.DeadlineExceeded, "auth token expired")
	case errors.Is(err, useoauth.ErrInvalidVerificationCode):
		return status.Error(codes.InvalidArgument, "invalid verification code")
	case errors.Is(err, useoauth.ErrSessionNotFound):
		return status.Error(codes.NotFound, "session not found")
	case errors.Is(err, useoauth.ErrSessionExpired):
		return status.Error(codes.DeadlineExceeded, "session expired")
	case errors.Is(err, useoauth.ErrInvalidEmail):
		return status.Error(codes.InvalidArgument, "invalid email format")
	default:
		return status.Error(codes.Internal, fmt.Sprintf("internal error: %v", err))
	}
}
