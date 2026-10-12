ALTER TABLE investment_nav_watches ADD COLUMN IF NOT EXISTS portfolio_managed boolean NOT NULL DEFAULT false;
ALTER TABLE investment_nav_watches ADD COLUMN IF NOT EXISTS enabled boolean NOT NULL DEFAULT true;
ALTER TABLE investment_nav_jobs DROP CONSTRAINT IF EXISTS investment_nav_jobs_status_check;
ALTER TABLE investment_nav_jobs ADD CONSTRAINT investment_nav_jobs_status_check CHECK(status IN ('pending','running','ready','error','cancelled'));
-- Existing subscriptions have unknown ownership and stay unmanaged. Migration
-- replay must never turn an explicitly retained/internal watch into managed.
