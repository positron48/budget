DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_groups) OR EXISTS(SELECT 1 FROM investment_group_revisions) OR EXISTS(SELECT 1 FROM investment_group_requests) THEN
  RAISE EXCEPTION 'investment groups contain user data' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE IF EXISTS investment_group_requests;
DROP TABLE IF EXISTS investment_group_revisions;
DROP TABLE IF EXISTS investment_groups;
