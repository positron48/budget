ALTER TABLE investment_nav_jobs ADD COLUMN IF NOT EXISTS claimed_generation bigint NOT NULL DEFAULT 0 CHECK(claimed_generation>=0);
ALTER TABLE investment_nav_jobs ADD COLUMN IF NOT EXISTS requested_generation bigint NOT NULL DEFAULT 0 CHECK(requested_generation>=0);
CREATE TABLE IF NOT EXISTS investment_nav_watches (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 actor_id uuid NOT NULL REFERENCES users(id),
 series_key text NOT NULL,
 request jsonb NOT NULL,
 from_date date NOT NULL,
 to_date date NOT NULL,
 cursor_generation bigint NOT NULL DEFAULT 0 CHECK(cursor_generation>=0),
 PRIMARY KEY(tenant_id,actor_id,series_key),
 CHECK(to_date>=from_date AND to_date-from_date<=3660)
);
