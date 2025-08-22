package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/positron48/budget/internal/domain"
	"github.com/redis/go-redis/v9"
)

type OAuthCache struct {
	client *Client
}

func NewOAuthCache(client *Client) *OAuthCache {
	return &OAuthCache{client: client}
}

// StoreAuthToken сохраняет временный токен в Redis
func (c *OAuthCache) StoreAuthToken(ctx context.Context, token domain.OAuthAuthToken) error {
	key := fmt.Sprintf("oauth:auth_token:%s", token.AuthToken)

	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to marshal auth token: %w", err)
	}

	ttl := time.Until(token.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("token already expired")
	}

	err = c.client.Client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store auth token in redis: %w", err)
	}

	return nil
}

// GetAuthToken получает временный токен из Redis
func (c *OAuthCache) GetAuthToken(ctx context.Context, authToken string) (domain.OAuthAuthToken, error) {
	key := fmt.Sprintf("oauth:auth_token:%s", authToken)

	data, err := c.client.Client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return domain.OAuthAuthToken{}, fmt.Errorf("auth token not found in cache")
		}
		return domain.OAuthAuthToken{}, fmt.Errorf("failed to get auth token from redis: %w", err)
	}

	var token domain.OAuthAuthToken
	err = json.Unmarshal(data, &token)
	if err != nil {
		return domain.OAuthAuthToken{}, fmt.Errorf("failed to unmarshal auth token: %w", err)
	}

	return token, nil
}

// UpdateAuthToken обновляет временный токен в Redis
func (c *OAuthCache) UpdateAuthToken(ctx context.Context, authToken string, token domain.OAuthAuthToken) error {
	key := fmt.Sprintf("oauth:auth_token:%s", authToken)

	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to marshal auth token: %w", err)
	}

	ttl := time.Until(token.ExpiresAt)
	if ttl <= 0 {
		// Если токен истек, удаляем его
		return c.DeleteAuthToken(ctx, authToken)
	}

	err = c.client.Client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to update auth token in redis: %w", err)
	}

	return nil
}

// DeleteAuthToken удаляет временный токен из Redis
func (c *OAuthCache) DeleteAuthToken(ctx context.Context, authToken string) error {
	key := fmt.Sprintf("oauth:auth_token:%s", authToken)

	err := c.client.Client.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete auth token from redis: %w", err)
	}

	return nil
}

// StoreVerificationCode сохраняет код подтверждения в Redis
func (c *OAuthCache) StoreVerificationCode(ctx context.Context, authToken, code string, ttl time.Duration) error {
	key := fmt.Sprintf("oauth:verification_code:%s", authToken)

	err := c.client.Client.Set(ctx, key, code, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store verification code in redis: %w", err)
	}

	return nil
}

// GetVerificationCode получает код подтверждения из Redis
func (c *OAuthCache) GetVerificationCode(ctx context.Context, authToken string) (string, error) {
	key := fmt.Sprintf("oauth:verification_code:%s", authToken)

	code, err := c.client.Client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", fmt.Errorf("verification code not found in cache")
		}
		return "", fmt.Errorf("failed to get verification code from redis: %w", err)
	}

	return code, nil
}

// DeleteVerificationCode удаляет код подтверждения из Redis
func (c *OAuthCache) DeleteVerificationCode(ctx context.Context, authToken string) error {
	key := fmt.Sprintf("oauth:verification_code:%s", authToken)

	err := c.client.Client.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete verification code from redis: %w", err)
	}

	return nil
}

// StoreTelegramSession сохраняет сессию Telegram в Redis
func (c *OAuthCache) StoreTelegramSession(ctx context.Context, session domain.TelegramSession) error {
	key := fmt.Sprintf("oauth:telegram_session:%s", session.SessionID)

	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram session: %w", err)
	}

	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("session already expired")
	}

	err = c.client.Client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store telegram session in redis: %w", err)
	}

	return nil
}

// GetTelegramSession получает сессию Telegram из Redis
func (c *OAuthCache) GetTelegramSession(ctx context.Context, sessionID string) (domain.TelegramSession, error) {
	key := fmt.Sprintf("oauth:telegram_session:%s", sessionID)

	data, err := c.client.Client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return domain.TelegramSession{}, fmt.Errorf("telegram session not found in cache")
		}
		return domain.TelegramSession{}, fmt.Errorf("failed to get telegram session from redis: %w", err)
	}

	var session domain.TelegramSession
	err = json.Unmarshal(data, &session)
	if err != nil {
		return domain.TelegramSession{}, fmt.Errorf("failed to unmarshal telegram session: %w", err)
	}

	return session, nil
}

// DeleteTelegramSession удаляет сессию Telegram из Redis
func (c *OAuthCache) DeleteTelegramSession(ctx context.Context, sessionID string) error {
	key := fmt.Sprintf("oauth:telegram_session:%s", sessionID)

	err := c.client.Client.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete telegram session from redis: %w", err)
	}

	return nil
}

// IncrementRateLimit увеличивает счетчик rate limit в Redis
func (c *OAuthCache) IncrementRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) (int64, error) {
	key := fmt.Sprintf("oauth:rate_limit:%s:%s:%d", telegramUserID, action, windowStart.Unix())

	count, err := c.client.Client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to increment rate limit in redis: %w", err)
	}

	// Устанавливаем TTL для ключа (1 час)
	if count == 1 {
		err = c.client.Client.Expire(ctx, key, time.Hour).Err()
		if err != nil {
			return 0, fmt.Errorf("failed to set TTL for rate limit key: %w", err)
		}
	}

	return count, nil
}

// GetRateLimit получает текущий счетчик rate limit из Redis
func (c *OAuthCache) GetRateLimit(ctx context.Context, telegramUserID, action string, windowStart time.Time) (int64, error) {
	key := fmt.Sprintf("oauth:rate_limit:%s:%s:%d", telegramUserID, action, windowStart.Unix())

	count, err := c.client.Client.Get(ctx, key).Int64()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get rate limit from redis: %w", err)
	}

	return count, nil
}

// StoreAccountBlock сохраняет блокировку аккаунта в Redis
func (c *OAuthCache) StoreAccountBlock(ctx context.Context, block domain.AccountBlock) error {
	key := fmt.Sprintf("oauth:account_block:%s", block.TelegramUserID)

	data, err := json.Marshal(block)
	if err != nil {
		return fmt.Errorf("failed to marshal account block: %w", err)
	}

	var ttl time.Duration
	if block.ExpiresAt != nil {
		ttl = time.Until(*block.ExpiresAt)
		if ttl <= 0 {
			return fmt.Errorf("block already expired")
		}
	} else {
		// Если нет времени истечения, устанавливаем TTL 24 часа
		ttl = 24 * time.Hour
	}

	err = c.client.Client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store account block in redis: %w", err)
	}

	return nil
}

// GetAccountBlock получает блокировку аккаунта из Redis
func (c *OAuthCache) GetAccountBlock(ctx context.Context, telegramUserID string) (*domain.AccountBlock, error) {
	key := fmt.Sprintf("oauth:account_block:%s", telegramUserID)

	data, err := c.client.Client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get account block from redis: %w", err)
	}

	var block domain.AccountBlock
	err = json.Unmarshal(data, &block)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal account block: %w", err)
	}

	return &block, nil
}

// DeleteAccountBlock удаляет блокировку аккаунта из Redis
func (c *OAuthCache) DeleteAccountBlock(ctx context.Context, telegramUserID string) error {
	key := fmt.Sprintf("oauth:account_block:%s", telegramUserID)

	err := c.client.Client.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete account block from redis: %w", err)
	}

	return nil
}

// CleanupExpiredKeys очищает истекшие ключи (вызывается периодически)
func (c *OAuthCache) CleanupExpiredKeys(ctx context.Context) error {
	// Redis автоматически удаляет ключи с TTL, поэтому здесь можно добавить
	// дополнительную логику очистки, если необходимо

	// Например, можно удалить старые ключи rate limit
	pattern := "oauth:rate_limit:*"
	keys, err := c.client.Client.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get rate limit keys: %w", err)
	}

	for _, key := range keys {
		// Проверяем TTL ключа
		ttl, err := c.client.Client.TTL(ctx, key).Result()
		if err != nil {
			continue
		}

		// Если TTL отрицательный (ключ истек), удаляем его
		if ttl < 0 {
			c.client.Client.Del(ctx, key)
		}
	}

	return nil
}
