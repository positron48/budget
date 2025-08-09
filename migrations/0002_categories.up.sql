-- Categories and i18n
CREATE TABLE IF NOT EXISTS categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    kind category_kind NOT NULL,
    code TEXT NOT NULL,
    parent_id UUID REFERENCES categories(id) ON DELETE SET NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, kind, code)
);

CREATE INDEX IF NOT EXISTS idx_categories_tenant ON categories(tenant_id);
CREATE INDEX IF NOT EXISTS idx_categories_kind ON categories(kind);

CREATE TABLE IF NOT EXISTS category_i18n (
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    locale TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    PRIMARY KEY (category_id, locale)
);


