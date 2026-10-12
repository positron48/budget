CREATE TABLE IF NOT EXISTS investment_archive_restores (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 source_sha256 text NOT NULL CHECK(source_sha256 ~ '^[0-9a-f]{64}$'),
 actor_id uuid NOT NULL REFERENCES users(id),
 source_created_at timestamptz NOT NULL,
 restored_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 verification jsonb NOT NULL,
 PRIMARY KEY(tenant_id,source_sha256)
);
