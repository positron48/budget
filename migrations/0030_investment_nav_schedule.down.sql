DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_nav_watches) THEN
  RAISE EXCEPTION 'NAV subscription history must be preserved' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE investment_nav_watches;
ALTER TABLE investment_nav_jobs DROP COLUMN requested_generation;
ALTER TABLE investment_nav_jobs DROP COLUMN claimed_generation;
