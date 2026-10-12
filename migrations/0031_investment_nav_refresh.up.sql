ALTER TABLE investment_nav_watches ADD COLUMN IF NOT EXISTS refresh_requested_at timestamptz;
