ALTER TABLE investment_nav_watches ADD COLUMN IF NOT EXISTS extension_limited boolean NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS investment_portfolios_rolling ON investment_portfolios(tenant_id,actor_id,(data->>'SeriesKey')) WHERE data->>'AutoExtend'='true' AND COALESCE(data->>'Archived','false')='false';
