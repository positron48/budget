-- Refuse rollback while native-provider evidence remains. Never delete it.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM market_provider_observations WHERE provider<>'tinvest')
 OR EXISTS(SELECT 1 FROM market_data_sources WHERE provider<>'tinvest')
 OR EXISTS(SELECT 1 FROM market_provider_instruments WHERE provider<>'tinvest')
 OR EXISTS(SELECT 1 FROM market_data_jobs WHERE provider<>'tinvest')
 OR EXISTS(SELECT 1 FROM market_bond_snapshots WHERE provider<>'tinvest') THEN
  RAISE EXCEPTION 'native history source evidence prevents rollback' USING ERRCODE='55000';
 END IF;
END $$;
DROP TRIGGER nav_generation_market_observations_insert ON market_provider_observations;
DROP TRIGGER nav_generation_market_observations_update ON market_provider_observations;
DROP TRIGGER nav_generation_market_observations_delete ON market_provider_observations;
CREATE TRIGGER nav_generation_market_provider_observations AFTER INSERT OR UPDATE OR DELETE ON market_provider_observations
FOR EACH ROW EXECUTE FUNCTION bump_investment_valuation_generation();
DROP FUNCTION bump_market_observation_generation_batch();
ALTER TABLE market_provider_observations DROP CONSTRAINT market_provider_observations_provider_check;
ALTER TABLE market_provider_observations ADD CONSTRAINT market_provider_observations_provider_check CHECK(provider='tinvest');
ALTER TABLE market_data_sources DROP CONSTRAINT market_data_sources_provider_check;
ALTER TABLE market_data_sources ADD CONSTRAINT market_data_sources_provider_check CHECK(provider='tinvest');
ALTER TABLE market_provider_instruments DROP CONSTRAINT market_provider_instruments_provider_check;
ALTER TABLE market_provider_instruments ADD CONSTRAINT market_provider_instruments_provider_check CHECK(provider='tinvest');
ALTER TABLE market_data_jobs DROP CONSTRAINT market_data_jobs_provider_check;
ALTER TABLE market_data_jobs ADD CONSTRAINT market_data_jobs_provider_check CHECK(provider='tinvest');
ALTER TABLE market_bond_snapshots DROP CONSTRAINT market_bond_snapshots_provider_check;
ALTER TABLE market_bond_snapshots ADD CONSTRAINT market_bond_snapshots_provider_check CHECK(provider='tinvest');
