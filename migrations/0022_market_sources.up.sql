-- Operator-provisioned entitlement bindings, never credentials.
CREATE TABLE IF NOT EXISTS market_data_sources (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 provider text NOT NULL CHECK(provider='tinvest'),
 access_scope text NOT NULL CHECK(access_scope ~ '^[a-zA-Z0-9_.:-]{1,128}$'),
 title text NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
 enabled boolean NOT NULL DEFAULT true,
 PRIMARY KEY(tenant_id,provider,access_scope)
);
