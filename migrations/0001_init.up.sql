-- Extensions
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Enums
DO $$ BEGIN
    CREATE TYPE tenant_role AS ENUM ('owner', 'admin', 'member');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    CREATE TYPE category_kind AS ENUM ('income', 'expense');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    CREATE TYPE transaction_type AS ENUM ('income', 'expense');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- Tenants
CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    slug TEXT UNIQUE,
    default_currency_code VARCHAR(3) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Users
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    name TEXT,
    locale TEXT,
    password_hash TEXT NOT NULL,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ
);

-- User ↔ Tenant memberships
CREATE TABLE IF NOT EXISTS user_tenants (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    role tenant_role NOT NULL DEFAULT 'member',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (user_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_user_tenants_user ON user_tenants(user_id);
CREATE INDEX IF NOT EXISTS idx_user_tenants_tenant ON user_tenants(tenant_id);

-- Refresh tokens (hashed)
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    user_agent TEXT,
    ip TEXT
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);


