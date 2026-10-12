-- Reviewed payout schedules and statement links are evidence, not cash entries.
CREATE TABLE IF NOT EXISTS investment_payout_events (
 tenant_id UUID NOT NULL REFERENCES tenants(id),
 id UUID NOT NULL,
 version BIGINT NOT NULL CHECK(version>0),
 data JSONB NOT NULL,
 payload_hash TEXT NOT NULL,
 PRIMARY KEY(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS investment_payout_event_revisions (
 tenant_id UUID NOT NULL,
 event_id UUID NOT NULL,
 version BIGINT NOT NULL CHECK(version>0),
 data JSONB NOT NULL,
 actor_id UUID NOT NULL REFERENCES users(id),
 reason TEXT NOT NULL CHECK(length(reason)>0),
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,event_id,version),
 FOREIGN KEY(tenant_id,event_id) REFERENCES investment_payout_events(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS investment_payout_links (
 tenant_id UUID NOT NULL,
 operation_id UUID NOT NULL,
 ledger_id UUID NOT NULL,
 event_id UUID NOT NULL,
 version BIGINT NOT NULL CHECK(version>0),
 data JSONB NOT NULL,
 payload_hash TEXT NOT NULL,
 PRIMARY KEY(tenant_id,operation_id),
 FOREIGN KEY(tenant_id,ledger_id,operation_id) REFERENCES investment_operations(tenant_id,ledger_id,id),
 FOREIGN KEY(tenant_id,event_id) REFERENCES investment_payout_events(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS investment_payout_link_revisions (
 tenant_id UUID NOT NULL,
 operation_id UUID NOT NULL,
 version BIGINT NOT NULL CHECK(version>0),
 data JSONB NOT NULL,
 actor_id UUID NOT NULL REFERENCES users(id),
 reason TEXT NOT NULL CHECK(length(reason)>0),
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,operation_id,version),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES investment_payout_links(tenant_id,operation_id)
);
