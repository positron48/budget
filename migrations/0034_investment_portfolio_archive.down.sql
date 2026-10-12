DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_nav_watches WHERE portfolio_managed OR NOT enabled)
 OR EXISTS(SELECT 1 FROM investment_nav_jobs WHERE status='cancelled')
 OR EXISTS(SELECT 1 FROM investment_portfolios WHERE data->>'Archived'='true') THEN
  RAISE EXCEPTION 'Archive and subscription state must be preserved' USING ERRCODE='55000';
 END IF;
END $$;
ALTER TABLE investment_nav_jobs DROP CONSTRAINT investment_nav_jobs_status_check;
ALTER TABLE investment_nav_jobs ADD CONSTRAINT investment_nav_jobs_status_check CHECK(status IN ('pending','running','ready','error'));
ALTER TABLE investment_nav_watches DROP COLUMN enabled;
ALTER TABLE investment_nav_watches DROP COLUMN portfolio_managed;
