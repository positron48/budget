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
