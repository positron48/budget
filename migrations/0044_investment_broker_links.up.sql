-- A broker trade and its fee consume provider identities together, even after
-- the linked journal operation is voided. Observed versions remain immutable.
CREATE TABLE IF NOT EXISTS investment_broker_links (
 id UUID PRIMARY KEY,
 tenant_id UUID NOT NULL,
 snapshot_id UUID NOT NULL,
 ledger_id UUID NOT NULL,
 operation_id UUID NOT NULL,
 operation_version BIGINT NOT NULL CHECK(operation_version>0),
 reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 2000),
 actor_id UUID NOT NULL REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id),
 UNIQUE(tenant_id,ledger_id,operation_id),
 FOREIGN KEY(tenant_id,snapshot_id) REFERENCES investment_broker_snapshots(tenant_id,id),
 FOREIGN KEY(tenant_id,ledger_id,operation_id) REFERENCES investment_operations(tenant_id,ledger_id,id)
);
CREATE TABLE IF NOT EXISTS investment_broker_claims (
 tenant_id UUID NOT NULL,
 account_id TEXT NOT NULL,
 operation_id TEXT NOT NULL,
 content_sha256 TEXT NOT NULL CHECK(length(content_sha256)=64),
 link_id UUID NOT NULL,
 PRIMARY KEY(tenant_id,account_id,operation_id),
 FOREIGN KEY(tenant_id,link_id) REFERENCES investment_broker_links(tenant_id,id),
 FOREIGN KEY(tenant_id,account_id,operation_id,content_sha256)
 REFERENCES investment_broker_observations(tenant_id,account_id,operation_id,content_sha256)
);
