CREATE TABLE IF NOT EXISTS investment_portfolios (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 actor_id uuid NOT NULL REFERENCES users(id),
 version bigint NOT NULL CHECK(version>0),
 data jsonb NOT NULL,
 UNIQUE(tenant_id,actor_id,id)
);
CREATE INDEX IF NOT EXISTS investment_portfolios_owner ON investment_portfolios(tenant_id,actor_id,id);
CREATE TABLE IF NOT EXISTS investment_portfolio_revisions (
 tenant_id uuid NOT NULL,
 actor_id uuid NOT NULL,
 portfolio_id uuid NOT NULL,
 version bigint NOT NULL,
 data jsonb NOT NULL,
 PRIMARY KEY(tenant_id,actor_id,portfolio_id,version),
 FOREIGN KEY(tenant_id,actor_id,portfolio_id) REFERENCES investment_portfolios(tenant_id,actor_id,id)
);
CREATE TABLE IF NOT EXISTS investment_portfolio_requests (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 actor_id uuid NOT NULL REFERENCES users(id),
 request_key text NOT NULL,
 payload_hash text NOT NULL,
 response jsonb NOT NULL,
 PRIMARY KEY(tenant_id,actor_id,request_key)
);
