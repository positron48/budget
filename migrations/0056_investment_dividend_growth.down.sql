DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_dividend_histories) OR EXISTS(SELECT 1 FROM investment_dividend_history_revisions) THEN
  RAISE EXCEPTION 'Reviewed dividend history must be preserved' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE IF EXISTS investment_dividend_history_revisions;
DROP TABLE IF EXISTS investment_dividend_histories;
