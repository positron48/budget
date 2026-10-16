-- Disposable derived data only; no financial journal or provider fact changes.
CREATE TABLE IF NOT EXISTS investment_tracker_charts (
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 actor_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 scope_key TEXT NOT NULL CHECK (scope_key ~ '^[a-f0-9]{64}$'),
 basis_key TEXT NOT NULL CHECK (basis_key ~ '^[a-f0-9]{64}$'),
 request BYTEA NOT NULL CHECK (octet_length(request) <= 65536),
 projection BYTEA NOT NULL CHECK (octet_length(projection) <= 16777216),
 computed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,actor_id,scope_key)
);
CREATE INDEX IF NOT EXISTS investment_tracker_charts_refresh ON investment_tracker_charts(computed_at,requested_at);
