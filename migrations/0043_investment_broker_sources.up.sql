CREATE TABLE IF NOT EXISTS investment_broker_snapshots (
 id UUID PRIMARY KEY,
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 source_sha256 TEXT NOT NULL CHECK(length(source_sha256)=64),
 account_id TEXT NOT NULL CHECK(length(account_id) BETWEEN 1 AND 200),
 original_json BYTEA NOT NULL CHECK(octet_length(original_json) BETWEEN 1 AND 33554432),
 row_count INTEGER NOT NULL CHECK(row_count BETWEEN 0 AND 100000),
 created_by UUID NOT NULL REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,source_sha256)
);
-- Provider changes append a content version, never overwrite an observation.
CREATE TABLE IF NOT EXISTS investment_broker_observations (
 id UUID PRIMARY KEY,
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 account_id TEXT NOT NULL CHECK(length(account_id) BETWEEN 1 AND 200),
 operation_id TEXT NOT NULL CHECK(length(operation_id) BETWEEN 1 AND 200),
 content_sha256 TEXT NOT NULL CHECK(length(content_sha256)=64),
 raw_json BYTEA NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,account_id,operation_id,content_sha256)
);
CREATE TABLE IF NOT EXISTS investment_broker_snapshot_rows (
 tenant_id UUID NOT NULL,
 snapshot_id UUID NOT NULL,
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 observation_id UUID NOT NULL,
 PRIMARY KEY(tenant_id,snapshot_id,ordinal),
 UNIQUE(tenant_id,snapshot_id,observation_id),
 FOREIGN KEY(tenant_id,snapshot_id) REFERENCES investment_broker_snapshots(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,observation_id) REFERENCES investment_broker_observations(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS investment_broker_snapshots_tenant_time ON investment_broker_snapshots(tenant_id,created_at DESC,id);
