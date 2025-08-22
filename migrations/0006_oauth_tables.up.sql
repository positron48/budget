-- OAuth2 Flow для Telegram бота

-- Enum для статуса авторизации
DO $$ BEGIN
    CREATE TYPE oauth_auth_status AS ENUM ('pending', 'completed', 'expired', 'cancelled');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- Enum для действий в логах
DO $$ BEGIN
    CREATE TYPE oauth_action_type AS ENUM ('generate_link', 'verify_code', 'cancel', 'revoke_session');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- Enum для статуса в логах
DO $$ BEGIN
    CREATE TYPE oauth_log_status AS ENUM ('success', 'failed', 'expired');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- Временные токены авторизации (хранятся в Redis, но логируем в БД)
CREATE TABLE IF NOT EXISTS oauth_auth_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    auth_token TEXT NOT NULL UNIQUE,  -- Хеш токена
    email TEXT NOT NULL,
    telegram_user_id TEXT NOT NULL,
    verification_code TEXT NOT NULL,  -- 6-значный код
    status oauth_auth_status NOT NULL DEFAULT 'pending',
    ip_address TEXT,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_oauth_auth_tokens_telegram_user ON oauth_auth_tokens(telegram_user_id);
CREATE INDEX IF NOT EXISTS idx_oauth_auth_tokens_email ON oauth_auth_tokens(email);
CREATE INDEX IF NOT EXISTS idx_oauth_auth_tokens_expires_at ON oauth_auth_tokens(expires_at);
CREATE INDEX IF NOT EXISTS idx_oauth_auth_tokens_status ON oauth_auth_tokens(status);

-- Сессии Telegram бота
CREATE TABLE IF NOT EXISTS telegram_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id TEXT NOT NULL UNIQUE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    telegram_user_id TEXT NOT NULL,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    access_token_hash TEXT NOT NULL,  -- Хеш access token
    refresh_token_hash TEXT NOT NULL, -- Хеш refresh token
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_telegram_sessions_session_id ON telegram_sessions(session_id);
CREATE INDEX IF NOT EXISTS idx_telegram_sessions_telegram_user ON telegram_sessions(telegram_user_id);
CREATE INDEX IF NOT EXISTS idx_telegram_sessions_user ON telegram_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_telegram_sessions_expires_at ON telegram_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_telegram_sessions_active ON telegram_sessions(is_active);

-- Логи безопасности OAuth
CREATE TABLE IF NOT EXISTS oauth_auth_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT,
    telegram_user_id TEXT NOT NULL,
    ip_address TEXT,
    user_agent TEXT,
    action oauth_action_type NOT NULL,
    status oauth_log_status NOT NULL,
    error_message TEXT,
    auth_token_id UUID REFERENCES oauth_auth_tokens(id) ON DELETE SET NULL,
    session_id UUID REFERENCES telegram_sessions(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_oauth_auth_logs_telegram_user ON oauth_auth_logs(telegram_user_id);
CREATE INDEX IF NOT EXISTS idx_oauth_auth_logs_action ON oauth_auth_logs(action);
CREATE INDEX IF NOT EXISTS idx_oauth_auth_logs_status ON oauth_auth_logs(status);
CREATE INDEX IF NOT EXISTS idx_oauth_auth_logs_created_at ON oauth_auth_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_oauth_auth_logs_ip_address ON oauth_auth_logs(ip_address);

-- Rate limiting для генерации ссылок
CREATE TABLE IF NOT EXISTS oauth_rate_limits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    telegram_user_id TEXT NOT NULL,
    action TEXT NOT NULL,  -- 'generate_link', 'verify_code'
    window_start TIMESTAMPTZ NOT NULL,
    attempts_count INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(telegram_user_id, action, window_start)
);

CREATE INDEX IF NOT EXISTS idx_oauth_rate_limits_telegram_user ON oauth_rate_limits(telegram_user_id);
CREATE INDEX IF NOT EXISTS idx_oauth_rate_limits_window ON oauth_rate_limits(window_start);

-- Блокировки аккаунтов
CREATE TABLE IF NOT EXISTS oauth_account_blocks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    telegram_user_id TEXT NOT NULL UNIQUE,
    email TEXT,
    reason TEXT NOT NULL,  -- 'too_many_failed_attempts', 'suspicious_activity'
    blocked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_oauth_account_blocks_telegram_user ON oauth_account_blocks(telegram_user_id);
CREATE INDEX IF NOT EXISTS idx_oauth_account_blocks_email ON oauth_account_blocks(email);
CREATE INDEX IF NOT EXISTS idx_oauth_account_blocks_expires_at ON oauth_account_blocks(expires_at);
