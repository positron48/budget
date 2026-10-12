DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_cash_links) THEN
  RAISE EXCEPTION 'Cash bridge documents require a full Budget backup; rollback refused' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE IF EXISTS investment_cash_links;
DROP INDEX IF EXISTS asset_transfers_tenant_id_unique;
