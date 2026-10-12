-- One reviewed effective schedule per tenant-local journal instrument key.
-- Whole-schedule replacement is versioned so boundaries change atomically.
CREATE TABLE IF NOT EXISTS investment_market_mappings (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 instrument_key text NOT NULL CHECK(length(instrument_key) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK(version>0),
 data jsonb NOT NULL,
 PRIMARY KEY(tenant_id,instrument_key)
);
CREATE TABLE IF NOT EXISTS investment_market_mapping_revisions (
 tenant_id uuid NOT NULL,
 instrument_key text NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 request_key text NOT NULL CHECK(length(request_key) BETWEEN 1 AND 128),
 request_hash text NOT NULL,
 data jsonb NOT NULL,
 actor_id uuid NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 2000),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,instrument_key,version),
 UNIQUE(tenant_id,request_key),
 FOREIGN KEY(tenant_id,instrument_key) REFERENCES investment_market_mappings(tenant_id,instrument_key)
);
-- Durable, atomic invalidation; a projection consumer is implemented separately.
CREATE TABLE IF NOT EXISTS investment_market_mapping_changes (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id uuid NOT NULL,
 instrument_key text NOT NULL,
 version bigint NOT NULL,
 affected_from timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(tenant_id,instrument_key,version),
 FOREIGN KEY(tenant_id,instrument_key,version) REFERENCES investment_market_mapping_revisions(tenant_id,instrument_key,version)
);
CREATE INDEX IF NOT EXISTS investment_market_mapping_changes_cursor ON investment_market_mapping_changes(tenant_id,id);
