CREATE TABLE IF NOT EXISTS market_data_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 provider text NOT NULL CHECK(provider='tinvest'),
 access_scope text NOT NULL,
 instrument_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('last_price','daily_candle')),
 from_at timestamptz NOT NULL,
 to_at timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','ready','error')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 lease_token uuid,
 lease_until timestamptz,
 error_code text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 finished_at timestamptz,
 UNIQUE(tenant_id,id),
 CHECK((status='running')=(lease_token IS NOT NULL AND lease_until IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS market_data_jobs_active_request ON market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at) WHERE status IN ('pending','running');
CREATE INDEX IF NOT EXISTS market_data_jobs_claim ON market_data_jobs(tenant_id,provider,access_scope,available_at,created_at) WHERE status IN ('pending','running');
CREATE TABLE IF NOT EXISTS market_data_coverage (
 tenant_id uuid NOT NULL,
 job_id uuid NOT NULL,
 state text NOT NULL CHECK(state IN ('observed','empty_unexplained')),
 observation_count integer NOT NULL CHECK(observation_count>=0),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,job_id),
 FOREIGN KEY(tenant_id,job_id) REFERENCES market_data_jobs(tenant_id,id)
);
