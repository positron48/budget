CREATE TABLE IF NOT EXISTS investment_nav_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 actor_id uuid NOT NULL REFERENCES users(id),
 series_key text NOT NULL,
 request jsonb NOT NULL,
 report_date date NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','ready','error')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
 lease_token uuid,
 lease_until timestamptz,
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 error_code text NOT NULL DEFAULT '',
 CHECK ((status='running')=(lease_token IS NOT NULL AND lease_until IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS investment_nav_jobs_active ON investment_nav_jobs(tenant_id,actor_id,series_key,report_date) WHERE status IN ('pending','running');
CREATE INDEX IF NOT EXISTS investment_nav_jobs_pending ON investment_nav_jobs(tenant_id,available_at,created_at) WHERE status IN ('pending','running');
