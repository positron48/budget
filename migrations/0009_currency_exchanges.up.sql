CREATE TABLE IF NOT EXISTS currency_exchanges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    from_amount_numeric NUMERIC(18,2) NOT NULL CHECK (from_amount_numeric > 0),
    from_currency_code VARCHAR(3) NOT NULL,
    to_amount_numeric NUMERIC(18,2) NOT NULL CHECK (to_amount_numeric > 0),
    to_currency_code VARCHAR(3) NOT NULL,
    rate_numeric NUMERIC(28,12) GENERATED ALWAYS AS (to_amount_numeric / from_amount_numeric) STORED,
    occurred_at TIMESTAMPTZ NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_currency_code <> to_currency_code)
);

CREATE INDEX IF NOT EXISTS idx_currency_exchanges_tenant_date
    ON currency_exchanges(tenant_id, occurred_at DESC);
