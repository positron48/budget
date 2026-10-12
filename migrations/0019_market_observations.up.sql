-- Raw provider evidence is entitlement scoped. It is not a confirmed listing
-- mapping, a normalized NAV input, or proof of continuous historical coverage.
CREATE TABLE IF NOT EXISTS market_provider_observations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 provider text NOT NULL CHECK(provider='tinvest'),
 access_scope text NOT NULL CHECK(length(access_scope) BETWEEN 1 AND 128),
 instrument_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('last_price','daily_candle')),
 observed_at timestamptz NOT NULL,
 source_kind text NOT NULL CHECK(source_kind IN ('exchange','dealer')),
 adjustment text NOT NULL CHECK(adjustment IN ('provider_unspecified','unadjusted')),
 data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'),
 payload_hash text NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(tenant_id,id),
 UNIQUE(tenant_id,provider,access_scope,instrument_id,kind,observed_at,source_kind,adjustment)
);
CREATE TABLE IF NOT EXISTS market_provider_observation_revisions (
 tenant_id uuid NOT NULL,
 observation_id uuid NOT NULL,
 revision bigint NOT NULL,
 data jsonb NOT NULL,
 received_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,observation_id,revision),
 FOREIGN KEY(tenant_id,observation_id) REFERENCES market_provider_observations(tenant_id,id)
);
-- An atomic durable invalidation feed. Future projection workers acknowledge
-- changes after publishing a generation; updates must never disappear silently.
CREATE TABLE IF NOT EXISTS market_data_changes (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id uuid NOT NULL,
 observation_id uuid NOT NULL,
 revision bigint NOT NULL,
 affected_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(tenant_id,observation_id,revision),
 FOREIGN KEY(tenant_id,observation_id,revision) REFERENCES market_provider_observation_revisions(tenant_id,observation_id,revision)
);
CREATE INDEX IF NOT EXISTS market_data_changes_tenant_cursor ON market_data_changes(tenant_id,id);
