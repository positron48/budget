CREATE TABLE IF NOT EXISTS investment_benchmarks (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 benchmark_key text NOT NULL,
 price_profile_key text NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 PRIMARY KEY(tenant_id,benchmark_key),
 FOREIGN KEY(tenant_id,price_profile_key) REFERENCES investment_price_conventions(tenant_id,profile_key)
);
CREATE TABLE IF NOT EXISTS investment_benchmark_revisions (
 tenant_id uuid NOT NULL,
 benchmark_key text NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 data jsonb NOT NULL,
 payload_hash text NOT NULL,
 actor_id uuid NOT NULL REFERENCES users(id),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,benchmark_key,version),
 FOREIGN KEY(tenant_id,benchmark_key) REFERENCES investment_benchmarks(tenant_id,benchmark_key)
);
