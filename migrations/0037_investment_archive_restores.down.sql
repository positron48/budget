DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_archive_restores) THEN
  RAISE EXCEPTION 'Archive restoration audit must be preserved' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE investment_archive_restores;
