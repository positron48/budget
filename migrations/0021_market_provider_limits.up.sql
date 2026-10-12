-- Credential fingerprints share one request gate across worker replicas/scopes.
-- No credential values are stored here.
CREATE TABLE IF NOT EXISTS market_provider_limits (
 credential_key text PRIMARY KEY CHECK(credential_key ~ '^[a-f0-9]{64}$'),
 next_at timestamptz NOT NULL
);
