DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_nav_jobs) THEN
  RAISE EXCEPTION 'NAV job history must be preserved' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE IF EXISTS investment_nav_jobs;
