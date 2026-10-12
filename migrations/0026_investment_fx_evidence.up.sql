CREATE TABLE IF NOT EXISTS investment_fx_policies (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 policy_key text NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 enabled boolean NOT NULL DEFAULT true,
 PRIMARY KEY(tenant_id,policy_key)
);
CREATE TABLE IF NOT EXISTS investment_fx_policy_revisions (
 tenant_id uuid NOT NULL,
 policy_key text NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 data jsonb NOT NULL,
 payload_hash text NOT NULL,
 actor_id uuid NOT NULL REFERENCES users(id),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,policy_key,version),
 FOREIGN KEY(tenant_id,policy_key) REFERENCES investment_fx_policies(tenant_id,policy_key)
);
CREATE TABLE IF NOT EXISTS investment_fx_points (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 policy_key text NOT NULL,
 policy_version bigint NOT NULL,
 from_currency text NOT NULL CHECK(from_currency ~ '^[A-Z]{3}$'),
 to_currency text NOT NULL CHECK(to_currency ~ '^[A-Z]{3}$' AND to_currency<>from_currency),
 effective_at timestamptz NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 data jsonb NOT NULL,
 payload_hash text NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(tenant_id,id),
 UNIQUE(tenant_id,policy_key,policy_version,from_currency,to_currency,effective_at),
 FOREIGN KEY(tenant_id,policy_key,policy_version) REFERENCES investment_fx_policy_revisions(tenant_id,policy_key,version)
);
CREATE TABLE IF NOT EXISTS investment_fx_point_revisions (
 tenant_id uuid NOT NULL,
 point_id uuid NOT NULL,
 revision bigint NOT NULL,
 data jsonb NOT NULL,
 recorded_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,point_id,revision),
 FOREIGN KEY(tenant_id,point_id) REFERENCES investment_fx_points(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS investment_fx_changes (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id uuid NOT NULL,
 policy_key text NOT NULL,
 policy_version bigint NOT NULL,
 point_id uuid,
 point_revision bigint,
 affected_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(tenant_id,policy_key,policy_version) REFERENCES investment_fx_policy_revisions(tenant_id,policy_key,version),
 FOREIGN KEY(tenant_id,point_id,point_revision) REFERENCES investment_fx_point_revisions(tenant_id,point_id,revision),
 CHECK((point_id IS NULL)=(point_revision IS NULL))
);
CREATE INDEX IF NOT EXISTS investment_fx_changes_cursor ON investment_fx_changes(tenant_id,id);
