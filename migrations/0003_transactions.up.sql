-- Transactions with multi-currency snapshot
CREATE TABLE IF NOT EXISTS transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
    type transaction_type NOT NULL,
    amount_numeric NUMERIC(18,2) NOT NULL,
    currency_code VARCHAR(3) NOT NULL,
    base_amount_numeric NUMERIC(18,2) NOT NULL,
    base_currency_code VARCHAR(3) NOT NULL,
    fx_rate NUMERIC(18,8),
    fx_provider TEXT,
    fx_as_of DATE,
    occurred_at TIMESTAMPTZ NOT NULL,
    comment TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_transactions_tenant ON transactions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_transactions_date ON transactions(tenant_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_transactions_category ON transactions(tenant_id, category_id);
CREATE INDEX IF NOT EXISTS idx_transactions_type ON transactions(tenant_id, type, occurred_at);


