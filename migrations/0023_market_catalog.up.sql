CREATE TABLE IF NOT EXISTS market_provider_instruments (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 provider text NOT NULL CHECK(provider='tinvest'),
 access_scope text NOT NULL,
 instrument_id uuid NOT NULL,
 data jsonb NOT NULL,
 payload_hash text NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,provider,access_scope,instrument_id)
);
CREATE TABLE IF NOT EXISTS market_provider_instrument_revisions (
 tenant_id uuid NOT NULL,
 provider text NOT NULL,
 access_scope text NOT NULL,
 instrument_id uuid NOT NULL,
 revision bigint NOT NULL,
 data jsonb NOT NULL,
 received_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,provider,access_scope,instrument_id,revision),
 FOREIGN KEY(tenant_id,provider,access_scope,instrument_id) REFERENCES market_provider_instruments(tenant_id,provider,access_scope,instrument_id)
);
