-- Do not destroy requested exports during rollback.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_export_jobs) THEN RAISE EXCEPTION 'investment exports exist' USING ERRCODE='55000'; END IF;
END $$;
DROP TABLE investment_export_jobs;
