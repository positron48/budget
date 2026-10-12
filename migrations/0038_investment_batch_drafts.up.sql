CREATE TABLE IF NOT EXISTS investment_batch_drafts (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 actor_id uuid NOT NULL REFERENCES users(id),
 id uuid NOT NULL,
 import_id uuid NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 request_key text NOT NULL CHECK(length(request_key) BETWEEN 1 AND 128),
 payload_hash text NOT NULL,
 data jsonb NOT NULL CHECK(octet_length(data::text)<=20971520),
 preview jsonb,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,actor_id,id),
 UNIQUE(tenant_id,request_key),
 FOREIGN KEY(tenant_id,import_id) REFERENCES investment_imports(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS investment_batch_drafts_owner ON investment_batch_drafts(tenant_id,actor_id,id);
