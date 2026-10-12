ALTER TABLE market_data_jobs ADD COLUMN IF NOT EXISTS priority SMALLINT NOT NULL DEFAULT 20 CHECK(priority BETWEEN 0 AND 100);
DROP INDEX IF EXISTS market_data_jobs_claim;
CREATE INDEX market_data_jobs_claim ON market_data_jobs(tenant_id,provider,access_scope,priority,available_at,created_at) WHERE status IN ('pending','running');
CREATE TABLE IF NOT EXISTS market_data_subscriptions (
 tenant_id UUID NOT NULL,
 provider TEXT NOT NULL,
 access_scope TEXT NOT NULL,
 instrument_id UUID NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('last_price','daily_candle','bond_facts')),
 requested_until TIMESTAMPTZ NOT NULL DEFAULT '0001-01-01',
 position_demand BOOLEAN NOT NULL DEFAULT false,
 next_due TIMESTAMPTZ NOT NULL DEFAULT '0001-01-01',
 PRIMARY KEY(tenant_id,provider,access_scope,instrument_id,kind),
 FOREIGN KEY(tenant_id,provider,access_scope) REFERENCES market_data_sources(tenant_id,provider,access_scope)
);
CREATE INDEX IF NOT EXISTS investment_security_legs_market_demand
 ON investment_security_legs(tenant_id,instrument_key,ledger_id,effective_at) INCLUDE(quantity);
