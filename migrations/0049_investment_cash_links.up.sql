-- Budget documents and investment facts share one transaction and version.
CREATE UNIQUE INDEX IF NOT EXISTS asset_transfers_tenant_id_unique ON asset_transfers(tenant_id,id);
CREATE TABLE IF NOT EXISTS investment_cash_links (
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 transfer_id UUID NOT NULL,
 ledger_id UUID NOT NULL,
 operation_id UUID NOT NULL,
 data JSONB NOT NULL,
 revisions JSONB NOT NULL DEFAULT '[]'::jsonb,
 PRIMARY KEY(tenant_id,transfer_id),
 UNIQUE(tenant_id,operation_id),
 FOREIGN KEY(tenant_id,transfer_id) REFERENCES asset_transfers(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,ledger_id) REFERENCES investment_ledgers(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,ledger_id,operation_id) REFERENCES investment_operations(tenant_id,ledger_id,id) ON DELETE RESTRICT
);
