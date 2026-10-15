-- Provider namespaces stay separate even when they describe the same security.
ALTER TABLE market_provider_observations DROP CONSTRAINT market_provider_observations_provider_check;
ALTER TABLE market_provider_observations ADD CONSTRAINT market_provider_observations_provider_check CHECK(provider IN ('tinvest','moex','twelvedata'));
ALTER TABLE market_data_sources DROP CONSTRAINT market_data_sources_provider_check;
ALTER TABLE market_data_sources ADD CONSTRAINT market_data_sources_provider_check CHECK(provider IN ('tinvest','moex','twelvedata'));
ALTER TABLE market_provider_instruments DROP CONSTRAINT market_provider_instruments_provider_check;
ALTER TABLE market_provider_instruments ADD CONSTRAINT market_provider_instruments_provider_check CHECK(provider IN ('tinvest','moex','twelvedata'));
ALTER TABLE market_data_jobs DROP CONSTRAINT market_data_jobs_provider_check;
ALTER TABLE market_data_jobs ADD CONSTRAINT market_data_jobs_provider_check CHECK(provider IN ('tinvest','moex','twelvedata'));
ALTER TABLE market_bond_snapshots DROP CONSTRAINT market_bond_snapshots_provider_check;
ALTER TABLE market_bond_snapshots ADD CONSTRAINT market_bond_snapshots_provider_check CHECK(provider IN ('tinvest','moex','twelvedata'));

-- Bulk history must not update the same generation row once per daily candle.
-- Transition tables retain the earliest affected date for every tenant and both
-- sides of an update. A statement that changes no rows publishes no event.
CREATE OR REPLACE FUNCTION bump_market_observation_generation_batch() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE item record; gen bigint;
BEGIN
 IF TG_OP='INSERT' THEN
  FOR item IN SELECT tenant_id,min(observed_at) dirty FROM observation_new GROUP BY tenant_id LOOP
   INSERT INTO investment_valuation_generations(tenant_id,generation) VALUES(item.tenant_id,1)
   ON CONFLICT(tenant_id) DO UPDATE SET generation=investment_valuation_generations.generation+1 RETURNING generation INTO gen;
   INSERT INTO investment_nav_invalidations(tenant_id,generation,affected_from,source_table)
   VALUES(item.tenant_id,gen,item.dirty,'market_provider_observations');
  END LOOP;
 ELSIF TG_OP='UPDATE' THEN
  FOR item IN SELECT tenant_id,min(observed_at) dirty FROM (
   SELECT tenant_id,observed_at FROM observation_old UNION ALL SELECT tenant_id,observed_at FROM observation_new
  ) combined GROUP BY tenant_id LOOP
   INSERT INTO investment_valuation_generations(tenant_id,generation) VALUES(item.tenant_id,1)
   ON CONFLICT(tenant_id) DO UPDATE SET generation=investment_valuation_generations.generation+1 RETURNING generation INTO gen;
   INSERT INTO investment_nav_invalidations(tenant_id,generation,affected_from,source_table)
   VALUES(item.tenant_id,gen,item.dirty,'market_provider_observations');
  END LOOP;
 ELSE
  FOR item IN SELECT tenant_id,min(observed_at) dirty FROM observation_old GROUP BY tenant_id LOOP
   INSERT INTO investment_valuation_generations(tenant_id,generation) VALUES(item.tenant_id,1)
   ON CONFLICT(tenant_id) DO UPDATE SET generation=investment_valuation_generations.generation+1 RETURNING generation INTO gen;
   INSERT INTO investment_nav_invalidations(tenant_id,generation,affected_from,source_table)
   VALUES(item.tenant_id,gen,item.dirty,'market_provider_observations');
  END LOOP;
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS nav_generation_market_provider_observations ON market_provider_observations;
DROP TRIGGER IF EXISTS nav_generation_market_observations_insert ON market_provider_observations;
DROP TRIGGER IF EXISTS nav_generation_market_observations_update ON market_provider_observations;
DROP TRIGGER IF EXISTS nav_generation_market_observations_delete ON market_provider_observations;
CREATE TRIGGER nav_generation_market_observations_insert AFTER INSERT ON market_provider_observations
REFERENCING NEW TABLE AS observation_new FOR EACH STATEMENT EXECUTE FUNCTION bump_market_observation_generation_batch();
CREATE TRIGGER nav_generation_market_observations_update AFTER UPDATE ON market_provider_observations
REFERENCING OLD TABLE AS observation_old NEW TABLE AS observation_new FOR EACH STATEMENT EXECUTE FUNCTION bump_market_observation_generation_batch();
CREATE TRIGGER nav_generation_market_observations_delete AFTER DELETE ON market_provider_observations
REFERENCING OLD TABLE AS observation_old FOR EACH STATEMENT EXECUTE FUNCTION bump_market_observation_generation_batch();

-- Shared conservative quota for native free-plan workers. Fingerprints only;
-- credentials and source payloads never enter this table.
CREATE TABLE IF NOT EXISTS market_provider_daily_limits (
 credential_key TEXT NOT NULL CHECK(credential_key ~ '^[a-f0-9]{64}$'),
 day DATE NOT NULL,
 used INTEGER NOT NULL CHECK(used>0),
 PRIMARY KEY(credential_key,day)
);
