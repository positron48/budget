-- Preparation data is deliberately separate from posted financial operations.
-- An import must pass financial mapping/reconciliation before ledger activation.
CREATE TABLE IF NOT EXISTS investment_imports (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 source_sha256 TEXT NOT NULL CHECK (length(source_sha256)=64),
 filename TEXT NOT NULL CHECK (length(filename) BETWEEN 1 AND 255),
 original_csv BYTEA NOT NULL CHECK (octet_length(original_csv)<=20971520),
 state TEXT NOT NULL DEFAULT 'requires_mapping' CHECK (state='requires_mapping'),
 preview_hash TEXT NOT NULL,
 initial_payload_hash TEXT NOT NULL,
 row_count INTEGER NOT NULL CHECK(row_count BETWEEN 1 AND 100000),
 version BIGINT NOT NULL DEFAULT 1,
 created_by UUID NOT NULL REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,source_sha256)
);
CREATE TABLE IF NOT EXISTS investment_import_accounts (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL,
 import_id UUID NOT NULL,
 source_key TEXT NOT NULL CHECK(length(source_key) BETWEEN 1 AND 200),
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
 UNIQUE(tenant_id,import_id,id), UNIQUE(tenant_id,import_id,source_key),
 FOREIGN KEY(tenant_id,import_id) REFERENCES investment_imports(tenant_id,id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS investment_import_rows (
 tenant_id UUID NOT NULL,
 import_id UUID NOT NULL,
 record INTEGER NOT NULL CHECK(record>0),
 source_row_id TEXT NOT NULL,
 account_id UUID NOT NULL,
 source JSONB NOT NULL,
 preview JSONB NOT NULL,
 mapping_evidence TEXT NOT NULL DEFAULT '',
 initial_mapping JSONB NOT NULL,
 version BIGINT NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,import_id,record),
 UNIQUE(tenant_id,source_row_id),
 FOREIGN KEY(tenant_id,import_id) REFERENCES investment_imports(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,import_id,account_id) REFERENCES investment_import_accounts(tenant_id,import_id,id)
);
CREATE TABLE IF NOT EXISTS investment_import_mapping_revisions (
 id BIGSERIAL PRIMARY KEY,
 tenant_id UUID NOT NULL,
 import_id UUID NOT NULL,
 record INTEGER NOT NULL,
 from_account_id UUID NOT NULL,
 to_account_id UUID NOT NULL,
 version BIGINT NOT NULL,
 reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 2000),
 actor_id UUID NOT NULL REFERENCES users(id),
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,import_id,record,version),
 FOREIGN KEY(tenant_id,import_id,record) REFERENCES investment_import_rows(tenant_id,import_id,record),
 FOREIGN KEY(tenant_id,import_id,from_account_id) REFERENCES investment_import_accounts(tenant_id,import_id,id),
 FOREIGN KEY(tenant_id,import_id,to_account_id) REFERENCES investment_import_accounts(tenant_id,import_id,id)
);
CREATE INDEX IF NOT EXISTS investment_imports_tenant_time ON investment_imports(tenant_id,created_at DESC,id);
CREATE INDEX IF NOT EXISTS investment_import_rows_account ON investment_import_rows(tenant_id,import_id,account_id,record);
