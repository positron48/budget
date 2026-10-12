DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_ledgers WHERE metadata_version>1 OR archived OR broker<>'' OR account_type<>'other' OR comment<>'' OR default_currency<>'RUB') THEN
  RAISE EXCEPTION USING ERRCODE='55000', MESSAGE='ledger lifecycle facts exist; refusing destructive downgrade';
 END IF;
END $$;
DROP TABLE investment_ledger_revisions;
ALTER TABLE investment_ledgers DROP COLUMN broker, DROP COLUMN account_type, DROP COLUMN comment, DROP COLUMN default_currency, DROP COLUMN archived, DROP COLUMN metadata_version;
