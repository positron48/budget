-- Ephemeral exports are private artifacts, not portable domain facts.
CREATE TABLE IF NOT EXISTS investment_export_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 actor_id uuid NOT NULL REFERENCES users(id),
 request_key uuid NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','ready','error','expired')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz,
 expires_at timestamptz,
 lease_until timestamptz,
 lease_token uuid,
 attempts integer NOT NULL DEFAULT 0,
 error_code text NOT NULL DEFAULT '',
 payload bytea,
 sha256 text NOT NULL DEFAULT '',
 byte_size bigint NOT NULL DEFAULT 0,
 UNIQUE(tenant_id,actor_id,request_key),
 CHECK(byte_size BETWEEN 0 AND 268435456),
 CHECK(payload IS NULL OR octet_length(payload)=byte_size)
);
CREATE INDEX IF NOT EXISTS investment_export_jobs_claim ON investment_export_jobs(state,created_at) WHERE state IN ('pending','running');
CREATE INDEX IF NOT EXISTS investment_export_jobs_expiry ON investment_export_jobs(expires_at) WHERE payload IS NOT NULL;

-- Chunk reads must not decompress the full archive for each 256 KiB request.
ALTER TABLE investment_export_jobs ALTER COLUMN payload SET STORAGE EXTERNAL;
