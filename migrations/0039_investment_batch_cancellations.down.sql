DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_batch_cancellations) THEN
  RAISE EXCEPTION 'Import cancellation audit must be preserved' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE investment_batch_cancellations;
