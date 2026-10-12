DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_price_convention_revisions) THEN
  RAISE EXCEPTION 'Confirmed price conventions must be preserved' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE investment_price_convention_revisions;
DROP TABLE investment_price_conventions;
