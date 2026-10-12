CREATE TABLE IF NOT EXISTS investment_dividend_histories (
 tenant_id UUID NOT NULL REFERENCES tenants(id), id UUID NOT NULL,
 version BIGINT NOT NULL CHECK(version>0), data JSONB NOT NULL, payload_hash TEXT NOT NULL,
 PRIMARY KEY(tenant_id,id)
);
CREATE UNIQUE INDEX IF NOT EXISTS investment_dividend_identity ON investment_dividend_histories(tenant_id,(data->>'InstrumentKey'),(data->>'Currency'));
CREATE TABLE IF NOT EXISTS investment_dividend_history_revisions (
 tenant_id UUID NOT NULL, history_id UUID NOT NULL, version BIGINT NOT NULL CHECK(version>0), data JSONB NOT NULL,
 actor_id UUID NOT NULL REFERENCES users(id), reason TEXT NOT NULL CHECK(length(reason)>0),
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,history_id,version), FOREIGN KEY(tenant_id,history_id) REFERENCES investment_dividend_histories(tenant_id,id)
);
