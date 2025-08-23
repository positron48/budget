package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/positron48/budget/internal/domain"
	useauth "github.com/positron48/budget/internal/usecase/auth"
)

type OAuthRepo struct {
	db *Pool
}

func NewOAuthRepo(db *Pool) *OAuthRepo {
	return &OAuthRepo{db: db}
}

// CreateAuthToken создает временный токен авторизации
func (r *OAuthRepo) CreateAuthToken(ctx context.Context, token domain.OAuthAuthToken) error {
	query := `
		INSERT INTO oauth_auth_tokens (
			id, auth_token, email, telegram_user_id, verification_code, 
			status, ip_address, user_agent, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := r.db.DB.Exec(ctx, query,
		token.ID, token.AuthToken, token.Email, token.TelegramUserID,
		token.VerificationCode, token.Status, token.IPAddress, token.UserAgent,
		token.CreatedAt, token.ExpiresAt,
	)
	return err
}

// GetAuthTokenByToken получает токен по хешу
func (r *OAuthRepo) GetAuthTokenByToken(ctx context.Context, authToken string) (domain.OAuthAuthToken, error) {
	query := `
		SELECT id, auth_token, email, telegram_user_id, verification_code,
			   status, ip_address, user_agent, created_at, expires_at,
			   completed_at, cancelled_at
		FROM oauth_auth_tokens
		WHERE auth_token = $1
	`

	var token domain.OAuthAuthToken
	err := r.db.DB.QueryRow(ctx, query, authToken).Scan(
		&token.ID, &token.AuthToken, &token.Email, &token.TelegramUserID,
		&token.VerificationCode, &token.Status, &token.IPAddress, &token.UserAgent,
		&token.CreatedAt, &token.ExpiresAt, &token.CompletedAt, &token.CancelledAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.OAuthAuthToken{}, fmt.Errorf("auth token not found")
		}
		return domain.OAuthAuthToken{}, err
	}

	return token, nil
}

// UpdateAuthTokenStatus обновляет статус токена
func (r *OAuthRepo) UpdateAuthTokenStatus(ctx context.Context, authToken string, status domain.AuthStatus) error {
	query := `
		UPDATE oauth_auth_tokens
		SET status = $2, completed_at = CASE WHEN $2 = 'completed' THEN now() ELSE completed_at END,
		    cancelled_at = CASE WHEN $2 = 'cancelled' THEN now() ELSE cancelled_at END
		WHERE auth_token = $1
	`

	result, err := r.db.DB.Exec(ctx, query, authToken, status)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("auth token not found")
	}

	return nil
}

// CreateTelegramSession создает сессию Telegram
func (r *OAuthRepo) CreateTelegramSession(ctx context.Context, session domain.TelegramSession) error {
	query := `
		INSERT INTO telegram_sessions (
			id, session_id, user_id, telegram_user_id, tenant_id,
			access_token_hash, refresh_token_hash, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := r.db.DB.Exec(ctx, query,
		session.ID, session.SessionID, session.UserID, session.TelegramUserID,
		session.TenantID, session.AccessTokenHash, session.RefreshTokenHash,
		session.CreatedAt, session.ExpiresAt,
	)
	return err
}

// GetTelegramSessionByID получает сессию по ID
func (r *OAuthRepo) GetTelegramSessionByID(ctx context.Context, sessionID string) (domain.TelegramSession, error) {
	query := `
		SELECT id, session_id, user_id, telegram_user_id, tenant_id,
			   access_token_hash, refresh_token_hash, created_at, expires_at,
			   last_used_at, revoked_at, is_active
		FROM telegram_sessions
		WHERE session_id = $1 AND is_active = true
	`

	var session domain.TelegramSession
	err := r.db.DB.QueryRow(ctx, query, sessionID).Scan(
		&session.ID, &session.SessionID, &session.UserID, &session.TelegramUserID,
		&session.TenantID, &session.AccessTokenHash, &session.RefreshTokenHash,
		&session.CreatedAt, &session.ExpiresAt, &session.LastUsedAt, &session.RevokedAt,
		&session.IsActive,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.TelegramSession{}, fmt.Errorf("session not found")
		}
		return domain.TelegramSession{}, err
	}

	return session, nil
}

// UpdateTelegramSessionLastUsed обновляет время последнего использования
func (r *OAuthRepo) UpdateTelegramSessionLastUsed(ctx context.Context, sessionID string) error {
	query := `
		UPDATE telegram_sessions
		SET last_used_at = now()
		WHERE session_id = $1
	`

	result, err := r.db.DB.Exec(ctx, query, sessionID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("session not found")
	}

	return nil
}

// RevokeTelegramSession отзывает сессию
func (r *OAuthRepo) RevokeTelegramSession(ctx context.Context, sessionID string) error {
	query := `
		UPDATE telegram_sessions
		SET revoked_at = now(), is_active = false
		WHERE session_id = $1
	`

	result, err := r.db.DB.Exec(ctx, query, sessionID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("session not found")
	}

	return nil
}

// ListTelegramSessions получает список активных сессий пользователя
func (r *OAuthRepo) ListTelegramSessions(ctx context.Context, telegramUserID string) ([]domain.TelegramSession, error) {
	query := `
		SELECT id, session_id, user_id, telegram_user_id, tenant_id,
			   access_token_hash, refresh_token_hash, created_at, expires_at,
			   last_used_at, revoked_at, is_active
		FROM telegram_sessions
		WHERE telegram_user_id = $1 AND is_active = true
		ORDER BY created_at DESC
	`

	rows, err := r.db.DB.Query(ctx, query, telegramUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []domain.TelegramSession
	for rows.Next() {
		var session domain.TelegramSession
		err := rows.Scan(
			&session.ID, &session.SessionID, &session.UserID, &session.TelegramUserID,
			&session.TenantID, &session.AccessTokenHash, &session.RefreshTokenHash,
			&session.CreatedAt, &session.ExpiresAt, &session.LastUsedAt, &session.RevokedAt,
			&session.IsActive,
		)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// CreateAuthLog создает запись в логе безопасности
func (r *OAuthRepo) CreateAuthLog(ctx context.Context, log domain.AuthLogEntry) error {
	query := `
		INSERT INTO oauth_auth_logs (
			id, email, telegram_user_id, ip_address, user_agent,
			action, status, error_message, auth_token_id, session_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err := r.db.DB.Exec(ctx, query,
		log.ID, log.Email, log.TelegramUserID, log.IPAddress, log.UserAgent,
		log.Action, log.Status, log.ErrorMessage, log.AuthTokenID, log.SessionID,
		log.CreatedAt,
	)
	return err
}

// GetAuthLogs получает логи безопасности
func (r *OAuthRepo) GetAuthLogs(ctx context.Context, telegramUserID string, limit, offset int) ([]domain.AuthLogEntry, int, error) {
	// Получаем общее количество
	countQuery := `
		SELECT COUNT(*)
		FROM oauth_auth_logs
		WHERE telegram_user_id = $1
	`

	var totalCount int
	err := r.db.DB.QueryRow(ctx, countQuery, telegramUserID).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Получаем записи
	query := `
		SELECT id, email, telegram_user_id, ip_address, user_agent,
			   action, status, error_message, auth_token_id, session_id, created_at
		FROM oauth_auth_logs
		WHERE telegram_user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.DB.Query(ctx, query, telegramUserID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []domain.AuthLogEntry
	for rows.Next() {
		var log domain.AuthLogEntry
		err := rows.Scan(
			&log.ID, &log.Email, &log.TelegramUserID, &log.IPAddress, &log.UserAgent,
			&log.Action, &log.Status, &log.ErrorMessage, &log.AuthTokenID, &log.SessionID,
			&log.CreatedAt,
		)
		if err != nil {
			return nil, 0, err
		}
		logs = append(logs, log)
	}

	return logs, totalCount, nil
}

// CheckRateLimit проверяет ограничение скорости
func (r *OAuthRepo) CheckRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) (int, error) {
	query := `
		SELECT attempts_count
		FROM oauth_rate_limits
		WHERE telegram_user_id = $1 AND action = $2 AND window_start = $3
	`

	var attemptsCount int
	err := r.db.DB.QueryRow(ctx, query, telegramUserID, action, windowStart).Scan(&attemptsCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}

	return attemptsCount, nil
}

// IncrementRateLimit увеличивает счетчик попыток
func (r *OAuthRepo) IncrementRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) error {
	query := `
		INSERT INTO oauth_rate_limits (telegram_user_id, action, window_start, attempts_count)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (telegram_user_id, action, window_start)
		DO UPDATE SET attempts_count = oauth_rate_limits.attempts_count + 1,
		              updated_at = now()
	`

	_, err := r.db.DB.Exec(ctx, query, telegramUserID, action, windowStart)
	return err
}

// CheckAccountBlock проверяет блокировку аккаунта
func (r *OAuthRepo) CheckAccountBlock(ctx context.Context, telegramUserID string) (*domain.AccountBlock, error) {
	query := `
		SELECT id, telegram_user_id, email, reason, blocked_at, expires_at, is_active
		FROM oauth_account_blocks
		WHERE telegram_user_id = $1 AND is_active = true
		  AND (expires_at IS NULL OR expires_at > now())
	`

	var block domain.AccountBlock
	err := r.db.DB.QueryRow(ctx, query, telegramUserID).Scan(
		&block.ID, &block.TelegramUserID, &block.Email, &block.Reason,
		&block.BlockedAt, &block.ExpiresAt, &block.IsActive,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &block, nil
}

// CreateAccountBlock создает блокировку аккаунта
func (r *OAuthRepo) CreateAccountBlock(ctx context.Context, block domain.AccountBlock) error {
	query := `
		INSERT INTO oauth_account_blocks (
			id, telegram_user_id, email, reason, blocked_at, expires_at, is_active
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (telegram_user_id)
		DO UPDATE SET email = EXCLUDED.email, reason = EXCLUDED.reason,
		              blocked_at = EXCLUDED.blocked_at, expires_at = EXCLUDED.expires_at,
		              is_active = EXCLUDED.is_active
	`

	_, err := r.db.DB.Exec(ctx, query,
		block.ID, block.TelegramUserID, block.Email, block.Reason,
		block.BlockedAt, block.ExpiresAt, block.IsActive,
	)
	return err
}

// CleanupExpiredData очищает истекшие данные
func (r *OAuthRepo) CleanupExpiredData(ctx context.Context) error {
	// Очищаем истекшие токены авторизации
	_, err := r.db.DB.Exec(ctx, `
		DELETE FROM oauth_auth_tokens
		WHERE expires_at < now() AND status IN ('pending', 'expired')
	`)
	if err != nil {
		return err
	}

	// Очищаем истекшие сессии
	_, err = r.db.DB.Exec(ctx, `
		UPDATE telegram_sessions
		SET is_active = false, revoked_at = now()
		WHERE expires_at < now() AND is_active = true
	`)
	if err != nil {
		return err
	}

	// Очищаем старые записи rate limiting (старше 1 часа)
	_, err = r.db.DB.Exec(ctx, `
		DELETE FROM oauth_rate_limits
		WHERE window_start < now() - INTERVAL '1 hour'
	`)
	if err != nil {
		return err
	}

	// Очищаем истекшие блокировки
	_, err = r.db.DB.Exec(ctx, `
		UPDATE oauth_account_blocks
		SET is_active = false
		WHERE expires_at IS NOT NULL AND expires_at < now() AND is_active = true
	`)

	return err
}

// GetUserByEmail получает пользователя по email с его tenant memberships
func (r *OAuthRepo) GetUserByEmail(ctx context.Context, email string) (useauth.User, []useauth.TenantMembership, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u useauth.User
	err := r.db.DB.QueryRow(ctx,
		`SELECT id, email, name, locale, password_hash FROM users WHERE email=$1`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Locale, &u.PasswordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return useauth.User{}, nil, err
		}
		return useauth.User{}, nil, err
	}
	rows, err := r.db.DB.Query(ctx,
		`SELECT tenant_id, role, is_default FROM user_tenants WHERE user_id=$1`, u.ID,
	)
	if err != nil {
		return useauth.User{}, nil, err
	}
	defer rows.Close()
	var ms []useauth.TenantMembership
	for rows.Next() {
		var m useauth.TenantMembership
		if err := rows.Scan(&m.TenantID, &m.Role, &m.IsDefault); err != nil {
			return useauth.User{}, nil, err
		}
		ms = append(ms, m)
	}
	if err := rows.Err(); err != nil {
		return useauth.User{}, nil, err
	}
	return u, ms, nil
}
