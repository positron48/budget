-- A reviewed source row is consumed once, even if its operation is later voided.
CREATE TABLE IF NOT EXISTS investment_source_links (
 tenant_id UUID NOT NULL,
 import_id UUID NOT NULL,
 record INTEGER NOT NULL,
 source_version BIGINT NOT NULL CHECK(source_version>0),
 account_id UUID NOT NULL,
 ledger_id UUID NOT NULL,
 operation_id UUID NOT NULL,
 review_reason TEXT NOT NULL CHECK(length(review_reason) BETWEEN 1 AND 2000),
 actor_id UUID NOT NULL REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,import_id,record),
 UNIQUE(tenant_id,ledger_id,operation_id),
 FOREIGN KEY(tenant_id,import_id,record) REFERENCES investment_import_rows(tenant_id,import_id,record),
 FOREIGN KEY(tenant_id,import_id,account_id) REFERENCES investment_import_accounts(tenant_id,import_id,id),
 FOREIGN KEY(tenant_id,ledger_id,operation_id) REFERENCES investment_operations(tenant_id,ledger_id,id)
);
CREATE TABLE IF NOT EXISTS investment_import_ledger_bindings (
 tenant_id UUID NOT NULL,
 import_id UUID NOT NULL,
 account_id UUID NOT NULL,
 ledger_id UUID NOT NULL,
 PRIMARY KEY(tenant_id,import_id,account_id),
 FOREIGN KEY(tenant_id,import_id,account_id) REFERENCES investment_import_accounts(tenant_id,import_id,id),
 FOREIGN KEY(tenant_id,ledger_id) REFERENCES investment_ledgers(tenant_id,id)
);
