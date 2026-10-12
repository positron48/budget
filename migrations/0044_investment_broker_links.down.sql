DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_broker_links) THEN
  RAISE EXCEPTION 'Cannot remove broker provenance while links exist' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE investment_broker_claims;
DROP TABLE investment_broker_links;
