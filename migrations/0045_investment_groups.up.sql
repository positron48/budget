CREATE TABLE IF NOT EXISTS investment_groups (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
 version bigint NOT NULL CHECK(version>0),
 data jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS investment_group_revisions (
 tenant_id uuid NOT NULL REFERENCES investment_groups(tenant_id),
 version bigint NOT NULL CHECK(version>0),
 actor_id uuid NOT NULL REFERENCES users(id),
 reason text NOT NULL,
 data jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,version)
);
CREATE TABLE IF NOT EXISTS investment_group_requests (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 actor_id uuid NOT NULL REFERENCES users(id),
 request_key text NOT NULL,
 payload_hash text NOT NULL,
 response jsonb NOT NULL,
 PRIMARY KEY(tenant_id,actor_id,request_key)
);
