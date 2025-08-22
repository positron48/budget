-- Откат OAuth2 таблиц

DROP TABLE IF EXISTS oauth_account_blocks;
DROP TABLE IF EXISTS oauth_rate_limits;
DROP TABLE IF EXISTS oauth_auth_logs;
DROP TABLE IF EXISTS telegram_sessions;
DROP TABLE IF EXISTS oauth_auth_tokens;

DROP TYPE IF EXISTS oauth_log_status;
DROP TYPE IF EXISTS oauth_action_type;
DROP TYPE IF EXISTS oauth_auth_status;
