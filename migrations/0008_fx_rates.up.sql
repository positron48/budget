-- Foreign exchange rates (per currency pair, per day, per provider)
CREATE TABLE IF NOT EXISTS fx_rates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_currency_code VARCHAR(3) NOT NULL,
    to_currency_code VARCHAR(3) NOT NULL,
    rate NUMERIC(18,8) NOT NULL,
    as_of DATE NOT NULL,
    provider TEXT NOT NULL DEFAULT 'manual',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (from_currency_code, to_currency_code, as_of, provider)
);

CREATE INDEX IF NOT EXISTS idx_fx_rates_lookup
    ON fx_rates(from_currency_code, to_currency_code, as_of DESC);
