DROP TABLE IF EXISTS market_data_subscriptions;
DROP INDEX IF EXISTS investment_security_legs_market_demand;
DROP INDEX IF EXISTS market_data_jobs_claim;
ALTER TABLE market_data_jobs DROP COLUMN IF EXISTS priority;
CREATE INDEX market_data_jobs_claim ON market_data_jobs(tenant_id,provider,access_scope,available_at,created_at) WHERE status IN ('pending','running');
