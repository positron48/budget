-- FX rates storage
CREATE TABLE IF NOT EXISTS fx_rates (
    from_currency_code VARCHAR(3) NOT NULL,
    to_currency_code VARCHAR(3) NOT NULL,
    rate NUMERIC(18,8) NOT NULL,
    as_of DATE NOT NULL,
    provider TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (from_currency_code, to_currency_code, as_of, provider)
);


